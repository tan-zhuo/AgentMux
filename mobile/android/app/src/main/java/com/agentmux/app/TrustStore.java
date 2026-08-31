package com.agentmux.app;

import android.content.Context;
import android.content.SharedPreferences;
import android.net.Uri;
import android.net.http.SslCertificate;
import android.os.Bundle;

import java.io.ByteArrayInputStream;
import java.security.MessageDigest;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.util.Locale;

/**
 * The certificates this device has chosen to trust, one fingerprint per host.
 *
 * A self-signed serve has no authority to vouch for it, so the user is the
 * authority: they compare the fingerprint the app shows with the one the
 * server printed, and what they approve is stored here — exact bytes for an
 * exact host, nothing broader. The WebView asks on every TLS error; only a
 * certificate whose SHA-256 matches the stored pin for that host proceeds.
 */
final class TrustStore {

    private static final String PREFS = "agentmux.pins";

    private TrustStore() {}

    static void pin(Context ctx, String host, String fingerprint) {
        prefs(ctx).edit().putString(host, fingerprint).apply();
    }

    /** Whether the certificate behind a WebView SSL error is the pinned one. */
    static boolean matches(Context ctx, String url, SslCertificate cert) {
        String host = Uri.parse(url == null ? "" : url).getHost();
        if (host == null) return false;
        String want = prefs(ctx).getString(host, null);
        if (want == null) return false;
        String got = fingerprint(cert);
        return got != null && got.equals(want);
    }

    /** SHA-256 of the certificate, colon-separated uppercase hex. */
    static String fingerprint(SslCertificate cert) {
        try {
            return fingerprint(x509(cert).getEncoded());
        } catch (Exception e) {
            return null;
        }
    }

    static String fingerprint(byte[] der) throws Exception {
        byte[] sum = MessageDigest.getInstance("SHA-256").digest(der);
        StringBuilder sb = new StringBuilder(sum.length * 3);
        for (byte b : sum) {
            if (sb.length() > 0) sb.append(':');
            sb.append(String.format(Locale.ROOT, "%02X", b));
        }
        return sb.toString();
    }

    /**
     * SslCertificate hides its X509 before API 29; the saved-state bundle has
     * carried the DER bytes since API 1 and is the documented way under it.
     */
    private static X509Certificate x509(SslCertificate cert) throws Exception {
        if (android.os.Build.VERSION.SDK_INT >= 29) {
            X509Certificate x = cert.getX509Certificate();
            if (x != null) return x;
        }
        Bundle state = SslCertificate.saveState(cert);
        byte[] der = state.getByteArray("x509-certificate");
        if (der == null) throw new IllegalStateException("certificate bytes unavailable");
        CertificateFactory cf = CertificateFactory.getInstance("X.509");
        return (X509Certificate) cf.generateCertificate(new ByteArrayInputStream(der));
    }

    private static SharedPreferences prefs(Context ctx) {
        return ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }
}
