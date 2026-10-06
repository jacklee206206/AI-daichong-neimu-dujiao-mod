# 发布到 GitHub

建议仓库名：`evan-ai-shop-journey`。

建议描述：**一个零编程基础大一学生借助 Codex 改造 Dujiao-Next 的实践记录，包含文章、源码、托管分销、商品 API 和部署流程。**

## 先保存草稿

作者确认前，材料保存在本地发布包和 GitHub 私有仓库的 Release 草稿中，不对外公开。

Release 草稿显式上传的 `evan-ai-shop-journey-20261005.zip` 包含完整文章、截图、操作手册、部署流程与应用源码。解压后，`app/` 是应用，`docs/` 是文档，根目录 README 为阅读入口。GitHub 自动生成的 Source code 压缩包只对应仓库当时的文件树，不能代替这个完整附件。

## 确认后发布完整源码树

1. 作者审核文章、图片、首页开源说明及源码包后，确认公开发布。
2. 解压显式上传的源码包，在 `evan-ai-shop-journey` 目录打开终端；只上传这一目录，不上传其他工作区内容。
3. 运行公开包检查，再初始化与提交：

```sh
python3 scripts/check_public_package.py
git init -b main
git add .
git status --short
git commit -m "Publish Evan AI shop journey and customized source"
```

4. 若仓库已有草稿 README，将仓库克隆到新的目录，再把完整包中的文件复制进去并提交，保留已有历史。不要直接强制推送覆盖远端。
5. 从 GitHub 复制仓库地址，使用已授权的 Git 登录方式推送；确认在线 `app/`、`docs/`、`ops/` 和 `scripts/` 均齐全。
6. 将仓库设为 Public，并发布经审核的 Release。先提交完整源码，后公开，避免公开页面只有草稿说明。

以下命令仅适用于尚无提交的空仓库：

```sh
git remote add origin https://github.com/YOUR_USERNAME/evan-ai-shop-journey.git
git push -u origin main
```

`YOUR_USERNAME` 改为自己的 GitHub 用户名。

## 首页与分享入口

GitHub 自动显示根目录 README。读者可从首页打开经历、流程、代码导读和两份 Word 接入手册。公开后，文章入口为：

```text
https://github.com/YOUR_USERNAME/evan-ai-shop-journey/blob/main/docs/我的三天三夜.md
```

## 继续维护

保留 GPL v3、原项目来源和修改说明。新的业务改动写进变更记录，发布可执行程序时同时提供匹配源码。实际配置、客户资料、订单、卡密和凭据存放在自己的运行环境中。
