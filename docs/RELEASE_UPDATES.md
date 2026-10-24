# 版本发布与在线更新

默认发布源为 `linjianyan0229/linai-api`。应用不会安装其他项目的发布包，也不会自动安装或重启。管理员点击侧边栏版本号进入「版本与更新」，查看更新说明、选择版本并确认下载；安装成功后单独确认重启。

## 发布正式版本

修改 `backend/cmd/server/VERSION` 为比现有正式发布更高的三段版本号，例如 `0.3.55`，提交并推送到 `main`。

`Publish VERSION` 工作流验证版本号并创建不可移动的 `v0.3.55` 标签，然后直接调用 `Release` 可复用工作流。它构建嵌入前端的程序、各平台安装包、SHA256 校验文件和 `ghcr.io/linjianyan0229/linai-api` 镜像，全部构建产物验证通过后发布 GitHub Release。只改业务代码不会发布新版本；版本号文件控制发布时机。已有标签指向其他提交、重复或降低正式版本号均会被拒绝。

单独推送 `v*` 标签仍可发布。在 Actions 中也可手动运行 `Publish VERSION`：默认勾选 `dry_run`，验证构建而不创建标签、Release 或推送镜像。`simple_release` 只构建 Linux amd64，但也提供在线更新所需的安装包及校验文件。

工作流使用仓库自带的 `GITHUB_TOKEN`（contents/packages 写权限），无需部署 SSH 密钥或额外个人令牌。自动打标签后直接调用可复用工作流，避免依赖机器人推送再次触发工作流的行为。参考 [GitHub 工作流复用](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows) 和 [工作流触发规则](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)。

## 用户更新

管理员页面每 30 分钟检查一次，后端缓存 20 分钟，也可手动刷新。只展示比运行版本更新的稳定 Release，忽略草稿和预发布。安装必须明确指定版本；缺少当前平台的安装包或 `checksums.txt` 时不可安装。检查失败不会显示为最新版本。

只支持 Linux 正式构建在线安装。下载地址必须属于配置的 GitHub 仓库和所选标签；下载及解压有大小限制，SHA256 校验通过后仅提取根目录普通 `sub2api` 文件，并验证 ELF 架构。版本检查不会写入程序。安装失败保留旧程序；安装和重启有全局操作锁及幂等保护。

首次使用需要部署一次带新入口和启动器的版本。数据库迁移会按原有启动流程执行；更新前保留数据库与配置备份。此更新流程面向单实例应用；多实例部署应通过部署平台统一更新每个实例。旧版本数据库兼容性没有保证，因此不提供网页一键降级。

## Docker

Docker 镜像设置 `UPDATE_STORAGE_DIR=/app/data/updates`。已验证的新程序与原子版本指针保存在现有数据卷中，重启或重建容器后继续生效。镜像内启动器重新验证文件校验和；更新文件缺失或损坏时回退到镜像自带程序。更高或相同版本的新镜像优先于数据卷中的旧更新。程序及嵌入的网页一起更新，不会重写容器镜像。

不需要 Docker socket 或特权容器。首次 GHCR 包发布后，仓库维护者需要确认 GitHub Packages 中的包可见性适合其分发范围；公开部署使用公开包，也可用本地 Dockerfile 构建。Compose 默认使用本仓库镜像，可通过 `LINAI_IMAGE` 固定镜像版本。已有本地 `docker-compose.override.yml` 仍可使用本地构建。

如需停用数据卷中的覆盖版本，可停止应用后将 `updates/current.json` 重命名为 `current.json.disabled`，再启动镜像；务必先确认数据库兼容性。

## 原生 Linux

未配置 `UPDATE_STORAGE_DIR` 时，新程序在现有可执行文件所在文件系统内下载、校验和暂存。备份成功后原子替换，旧程序保存在 `.backup` 中。运行用户需要写程序目录，服务管理器需支持退出后重启；安装成功后手动确认重启。

可用 `UPDATE_REPOSITORY=owner/repository` 或 `update.repository` 改变发布源，默认始终为本仓库。代理沿用 `UPDATE_PROXY_URL`，需要访问限制时可配置 `UPDATE_GITHUB_TOKEN`；这些配置不返回浏览器。
