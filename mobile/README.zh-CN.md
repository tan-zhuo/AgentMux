# AgentMux 安卓应用

**独立运行**：APK 内嵌了完整的 Go 核心（和 Linux 服务器版同一个静态二进制，交叉编译到
Android），应用启动时由前台服务在本机拉起、WebView 连 `127.0.0.1`。SSH 直接从
平板/手机发起，配置与密钥存在设备本地——**不需要任何一台"常开的机器"**。

同一个启动页也保留了远程模式：在普通浏览器里打开（没有原生外壳）时，会改为询问一台
`agentmux --serve` 的地址。

## 直接下载（推荐）

GitHub 的 Release 自动附带 `agentmux-android.apk`（arm64），下载安装即可。
仓库配置了 `ANDROID_KEYSTORE`（base64 的 keystore）与 `ANDROID_KEYSTORE_PASSWORD`
两个 secret 时产出正式签名的包（可覆盖升级）；未配置时产出 debug 签名的包
（同样能装，但换签名升级前要先卸载）。

## 本地构建（可选，需要 Android Studio 与 Go）

```bash
# 1. 编译前端（核心会把它嵌进去）
cd frontend && npm ci && npm run build && cd ..

# 2. 把核心交叉编译进安卓工程
mkdir -p mobile/android/app/src/main/jniLibs/arm64-v8a
CGO_ENABLED=0 GOOS=android GOARCH=arm64 \
  go build -tags headless -o mobile/android/app/src/main/jniLibs/arm64-v8a/libagentmux.so .

# 3. 同步资源并构建
cd mobile && npm install && npx cap sync android
npx cap open android   # Build → Build APK
```

## 工作方式

- `CoreService`（前台服务，带常驻通知）用 `ProcessBuilder` 拉起
  `libagentmux.so --addr 127.0.0.1:8642`，访问令牌每台设备生成一次并保存。
  前台服务是刻意的：没有它，锁屏后安卓会冻结进程、掐断所有 SSH 连接。
- agent 本身活在远端 tmux 里，应用被杀也不影响它们——重开应用即重连。
- 换到"连远程 serve"：设置里的「连接模式」可以在本机核心和远程服务器之间切换，
  选择会被记住，之后每次启动都直接进所选模式。启动页上也有同样的入口（本机核心
  启动很慢或起不来时可以直接改连远程）。
