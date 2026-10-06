// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package integrationtest

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	cardsecretapp "github.com/dujiao-next/internal/modules/cardsecret/application"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	cardsecretgormstore "github.com/dujiao-next/internal/modules/cardsecret/infrastructure/gormstore"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func newHistoricalDedupFixture(t *testing.T) (*gorm.DB, *cardsecretapp.Service, uint, uint) {
	t.Helper()
	db := setupCardSecretServiceTestDB(t)
	product := productdomain.Product{
		CategoryID: 1, Slug: "historical-dedup-fixture",
		TitleJSON:       jsonmap.JSON{"zh-CN": "测试卡密"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(20)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto, IsActive: true,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	sku := productdomain.ProductSKU{
		ProductID: product.ID, SKUCode: productdomain.DefaultSKUCode,
		PriceAmount: money.FromDecimal(decimal.NewFromInt(20)), IsActive: true,
	}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewCardSecretService(cardsecretgormstore.New(db), cardsecretgormstore.NewBatch(db),
		productgormstore.NewProductStore(db), productgormstore.NewSKUStore(db))
	return db, svc, product.ID, sku.ID
}

func TestCardSecretImportDeduplicatesAllHistoricalStatuses(t *testing.T) {
	db, svc, productID, skuID := newHistoricalDedupFixture(t)
	now := time.Now()
	for _, status := range []string{cardsecretdomain.StatusAvailable, cardsecretdomain.StatusReserved, cardsecretdomain.StatusUsed, "deleted"} {
		secret := cardsecretdomain.Secret{ProductID: productID, SKUID: skuID, Secret: "FAKE-HISTORY-" + status, Status: status}
		if status == "deleted" {
			secret.Status = cardsecretdomain.StatusAvailable
			secret.DeletedAt = &now
		}
		if err := db.Create(&secret).Error; err != nil {
			t.Fatal(err)
		}
	}
	batch, created, err := svc.CreateCardSecretBatch(CreateCardSecretBatchInput{
		ProductID: productID, SKUID: skuID, BatchNo: "FAKE-HISTORY-IMPORT",
		Secrets: []string{"FAKE-HISTORY-available", "FAKE-HISTORY-reserved", "FAKE-HISTORY-used", "FAKE-HISTORY-deleted", " FAKE-NEW\nFAKE-NEW ", "FAKE-NEW-2"},
	})
	if err != nil || batch == nil || created != 2 || batch.TotalCount != 2 {
		t.Fatalf("historical dedup: created=%d batch=%v err=%v", created, batch, err)
	}
	var count int64
	if err := db.Model(&cardsecretdomain.Secret{}).Unscoped().Count(&count).Error; err != nil || count != 6 {
		t.Fatalf("historical inventory count: got=%d err=%v", count, err)
	}
	// Repeating an import must report zero newly created inventory, including
	// previously used/deleted entries; the empty batch remains an audit record.
	batch, created, err = svc.CreateCardSecretBatch(CreateCardSecretBatchInput{
		ProductID: productID, SKUID: skuID, BatchNo: "FAKE-HISTORY-REPEAT",
		Secrets: []string{"FAKE-NEW", "FAKE-HISTORY-used", "FAKE-HISTORY-deleted"},
	})
	if err != nil || batch == nil || batch.ID == 0 || created != 0 || batch.TotalCount != 0 {
		t.Fatalf("reimport: created=%d batch=%v err=%v", created, batch, err)
	}
}

func TestCardSecretImportDedupScopeAndExplicitOptOut(t *testing.T) {
	db, svc, productID, skuID := newHistoricalDedupFixture(t)
	for _, scope := range [][2]uint{{productID + 100, skuID}, {productID, skuID + 100}} {
		if err := db.Create(&cardsecretdomain.Secret{
			ProductID: scope[0], SKUID: scope[1], Secret: "FAKE-SCOPED", Status: cardsecretdomain.StatusUsed,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, created, err := svc.CreateCardSecretBatch(CreateCardSecretBatchInput{
		ProductID: productID, SKUID: skuID, BatchNo: "FAKE-SCOPED-FIRST", Secrets: []string{"FAKE-SCOPED"},
	})
	if err != nil || created != 1 {
		t.Fatalf("other product/SKU must not count as this inventory: created=%d err=%v", created, err)
	}
	keepDuplicates := false
	_, created, err = svc.CreateCardSecretBatch(CreateCardSecretBatchInput{
		ProductID: productID, SKUID: skuID, BatchNo: "FAKE-SCOPED-OPT-OUT", Secrets: []string{"FAKE-SCOPED", "FAKE-SCOPED"}, Deduplicate: &keepDuplicates,
	})
	if err != nil || created != 2 {
		t.Fatalf("explicit opt out must keep historical and input duplicates: created=%d err=%v", created, err)
	}
}

func TestCardSecretCSVImportDeduplicatesHistoricalInventory(t *testing.T) {
	_, svc, productID, skuID := newHistoricalDedupFixture(t)
	if _, _, err := svc.CreateCardSecretBatch(CreateCardSecretBatchInput{
		ProductID: productID, SKUID: skuID, BatchNo: "FAKE-CSV-ORIGINAL", Secrets: []string{"FAKE-CSV-OLD"},
	}); err != nil {
		t.Fatal(err)
	}
	batch, created, err := svc.ImportCardSecretCSV(ImportCardSecretCSVInput{
		ProductID: productID, SKUID: skuID, BatchNo: "FAKE-CSV-NEXT",
		File: newCardSecretCSVFileHeader(t, "secret\nFAKE-CSV-OLD\nFAKE-CSV-NEW\nFAKE-CSV-NEW\n"),
	})
	if err != nil || batch == nil || created != 1 || batch.TotalCount != 1 {
		t.Fatalf("CSV historical dedup: created=%d batch=%v err=%v", created, batch, err)
	}
}

func TestConcurrentCardSecretImportsDoNotCommitDuplicateInventory(t *testing.T) {
	db, svc, productID, skuID := newHistoricalDedupFixture(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			// SQLite can reject one competing writer; retry below then verify
			// only one inventory row exists regardless of the first outcomes.
			_, _, _ = svc.CreateCardSecretBatch(CreateCardSecretBatchInput{
				ProductID: productID, SKUID: skuID, BatchNo: fmt.Sprintf("FAKE-CONCURRENT-%d", i), Secrets: []string{"FAKE-CONCURRENT-CARD"},
			})
		}(i)
	}
	close(start)
	wg.Wait()
	if _, _, err := svc.CreateCardSecretBatch(CreateCardSecretBatchInput{
		ProductID: productID, SKUID: skuID, BatchNo: "FAKE-CONCURRENT-RETRY", Secrets: []string{"FAKE-CONCURRENT-CARD"},
	}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&cardsecretdomain.Secret{}).Where("product_id = ? AND sku_id = ?", productID, skuID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("concurrent duplicate inventory: count=%d err=%v", count, err)
	}
}
