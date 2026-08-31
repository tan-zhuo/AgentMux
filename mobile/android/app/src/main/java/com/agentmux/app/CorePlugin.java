package com.agentmux.app;

import android.content.Intent;
import android.net.Uri;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

import java.net.InetSocketAddress;
import java.security.cert.Certificate;
import java.security.cert.X509Certificate;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocket;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.TrustManager;
import javax.net.ssl.X509TrustManager;

/**
 * The native calls the connect page needs: start the embedded core and say
 * where it listens and how to authenticate. Its absence (in a plain browser)
 * is what tells the page to offer remote-serve mode instead.
 *
 * It also carries the certificate-trust conversation for a self-signed
 * remote serve: fetch the certificate's fingerprint so the page can show it,
 * and pin what the user approves — the WebView (MainActivity's client)
 * accepts exactly that certificate for that host from then on.
 */
@CapacitorPlugin(name = "Core")
public class CorePlugin extends Plugin {

    @PluginMethod
    public void getServerInfo(PluginCall call) {
        Intent svc = new Intent(getContext(), CoreService.class);
        getContext().startForegroundService(svc);

        JSObject out = new JSObject();
        out.put("port", CoreService.PORT);
        out.put("token", CoreService.token(getContext()));
        call.resolve(out);
    }

    /**
     * What the connect page shows when the boot fails: is the process alive,
     * and what did it last say. Without this a phone failure is a blank
     * shrug; with it the error explains itself.
     */
    @PluginMethod
    public void status(PluginCall call) {
        JSObject out = new JSObject();
        out.put("alive", CoreService.alive());
        out.put("log", CoreService.recentLog());
        call.resolve(out);
    }

    /**
     * Handshakes with an https URL and returns the SHA-256 fingerprint of the
     * certificate it presented, without trusting anything: nothing is sent
     * past the handshake, and nothing is stored. The page shows the value;
     * the user compares it with the server's log; trustCert seals it.
     */
    @PluginMethod
    public void probeCert(PluginCall call) {
        String url = call.getString("url", "");
        Uri uri = Uri.parse(url);
        String host = uri.getHost();
        int port = uri.getPort() > 0 ? uri.getPort() : 443;
        if (host == null || !"https".equals(uri.getScheme())) {
            call.reject("not an https address");
            return;
        }
        new Thread(() -> {
            try {
                TrustManager[] observeOnly = new TrustManager[] {
                    new X509TrustManager() {
                        public void checkClientTrusted(X509Certificate[] chain, String authType) {}
                        public void checkServerTrusted(X509Certificate[] chain, String authType) {}
                        public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
                    }
                };
                SSLContext ctx = SSLContext.getInstance("TLS");
                ctx.init(null, observeOnly, new java.security.SecureRandom());
                SSLSocketFactory factory = ctx.getSocketFactory();
                try (SSLSocket sock = (SSLSocket) factory.createSocket()) {
                    sock.connect(new InetSocketAddress(host, port), 4000);
                    sock.startHandshake();
                    Certificate[] chain = sock.getSession().getPeerCertificates();
                    if (chain.length == 0) {
                        call.reject("the server presented no certificate");
                        return;
                    }
                    JSObject out = new JSObject();
                    out.put("host", host);
                    out.put("fingerprint", TrustStore.fingerprint(chain[0].getEncoded()));
                    call.resolve(out);
                }
            } catch (Exception e) {
                call.reject("could not reach " + host + ": " + e.getMessage());
            }
        }).start();
    }

    /** Pins one fingerprint for one host — what the user just approved. */
    @PluginMethod
    public void trustCert(PluginCall call) {
        String host = call.getString("host", "");
        String fingerprint = call.getString("fingerprint", "");
        if (host == null || host.isEmpty() || fingerprint == null || fingerprint.isEmpty()) {
            call.reject("host and fingerprint are required");
            return;
        }
        TrustStore.pin(getContext(), host, fingerprint);
        call.resolve();
    }
}
