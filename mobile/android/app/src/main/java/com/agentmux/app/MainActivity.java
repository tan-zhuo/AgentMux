package com.agentmux.app;

import android.net.http.SslError;
import android.os.Bundle;
import android.webkit.SslErrorHandler;
import android.webkit.WebView;

import com.getcapacitor.BridgeActivity;
import com.getcapacitor.BridgeWebViewClient;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        registerPlugin(CorePlugin.class);
        super.onCreate(savedInstanceState);

        // A self-signed serve fails TLS in a WebView with no way for the page
        // to intervene — fetch() just fails, silently. This client is the
        // intervention: a certificate the user has pinned for this host (via
        // the connect page's fingerprint confirmation) proceeds; everything
        // else stays refused exactly as before.
        this.bridge.setWebViewClient(new BridgeWebViewClient(this.bridge) {
            @Override
            public void onReceivedSslError(WebView view, SslErrorHandler handler, SslError error) {
                if (TrustStore.matches(MainActivity.this, error.getUrl(), error.getCertificate())) {
                    handler.proceed();
                } else {
                    handler.cancel();
                }
            }
        });
    }
}
