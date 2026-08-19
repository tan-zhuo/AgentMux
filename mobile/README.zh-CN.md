# AgentMux 安卓 APK 壳

一个 Capacitor 工程：把 `agentmux --serve` 包成可安装的安卓应用（WebView 壳）。
服务器地址在**首次启动时输入**，应用会记住它——所以一个通用 APK 适用于所有人，
不需要为自己的服务器单独编译。iPad 不需要 APK——Safari 打开服务地址后
「分享 → 添加到主屏幕」即可。

## 直接下载（推荐）

GitHub 的 Release 会自动附带 `agentmux-android.apk`，下载安装即可。
仓库配置了 `ANDROID_KEYSTORE`（base64 的 keystore）和 `ANDROID_KEYSTORE_PASSWORD`
两个 secret 时产出正式签名的包（可覆盖升级）；没有配置时产出 debug 签名的包
（同样能装，但换签名升级前要先卸载）。

## 本地构建（可选，需要 Android Studio）

```bash
cd mobile
npm install
npx cap add android
npx cap sync android
# 允许明文 http（内网/VPN 地址无需证书）：
sed -i 's/<application /<application android:usesCleartextTraffic="true" /' \
  android/app/src/main/AndroidManifest.xml
npx cap open android   # Build → Build APK
```

## 服务端

在任意一台常开的机器上运行 Release 里的 `agentmux-server-linux-{amd64,arm64}`
（静态二进制，无任何依赖），或桌面版加 `--serve`：

```bash
./agentmux                       # 服务器版：裸启动即服务，默认 :8642
AGENTMUX_TOKEN=你的令牌 ./agentmux --addr 0.0.0.0:8642
```

首次启动打印访问令牌（也保存在数据目录 `serve-token`），在应用里输入一次即可。

## 说明

- 换服务器地址：清除应用数据即可回到地址输入页。
- 公网访问请放在 HTTPS 反向代理（Caddy/Nginx）后面。
