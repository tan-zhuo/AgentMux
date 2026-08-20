import type { CapacitorConfig } from '@capacitor/cli'

// The Android app is a thin WebView shell: a local connect page asks for the
// address of a running `agentmux --serve` once, remembers it, and navigates
// there. Nothing is baked in at build time, so one CI-built APK works for
// every user and every server.
const config: CapacitorConfig = {
  appId: 'com.agentmux.app',
  appName: 'AgentMux',
  webDir: 'www',
  server: {
    // The shell navigates to whatever address the user enters.
    allowNavigation: ['*'],
    // Plain http, so a LAN or VPN address works without a certificate.
    cleartext: true,
    // The connect page must fetch-probe http://127.0.0.1 (the embedded core)
    // and plain-http remote serves. Under the default https://localhost
    // origin those probes are mixed content and the WebView silently blocks
    // them — the core runs, the page never learns.
    androidScheme: 'http',
  },
  android: {
    backgroundColor: '#080a0f',
  },
}

export default config
