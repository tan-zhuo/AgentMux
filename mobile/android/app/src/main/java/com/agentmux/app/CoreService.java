package com.agentmux.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.Service;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.ServiceInfo;
import android.os.Build;
import android.os.IBinder;
import android.util.Log;

import java.io.File;
import java.security.SecureRandom;
import java.util.Map;

/**
 * Runs the embedded AgentMux core — the same static Go binary the Linux
 * server release ships, packaged as a native library — as a child process
 * bound to localhost. A foreground service, because the core holds the SSH
 * connections: without the notification Android would freeze the process the
 * moment the screen locks, and every connection with it. The agents
 * themselves live in remote tmux either way; this only decides whether the
 * viewer has to reconnect.
 */
public class CoreService extends Service {
    private static final String TAG = "AgentMuxCore";
    private static final String CHANNEL = "core";
    public static final int PORT = 8642;

    private Process core;

    /** The token the WebView authenticates with, minted once per install. */
    public static String token(android.content.Context ctx) {
        SharedPreferences prefs = ctx.getSharedPreferences("core", MODE_PRIVATE);
        String t = prefs.getString("token", null);
        if (t == null) {
            byte[] raw = new byte[24];
            new SecureRandom().nextBytes(raw);
            StringBuilder sb = new StringBuilder();
            for (byte b : raw) sb.append(String.format("%02x", b));
            t = sb.toString();
            prefs.edit().putString("token", t).apply();
        }
        return t;
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        startInForeground();
        startCore();
        return START_STICKY;
    }

    private void startInForeground() {
        NotificationManager nm = getSystemService(NotificationManager.class);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            nm.createNotificationChannel(new NotificationChannel(
                    CHANNEL, "AgentMux core", NotificationManager.IMPORTANCE_MIN));
        }
        Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                ? new Notification.Builder(this, CHANNEL)
                : new Notification.Builder(this);
        Notification n = b.setContentTitle("AgentMux")
                .setContentText("core running on this device")
                .setSmallIcon(R.mipmap.ic_launcher)
                .setOngoing(true)
                .build();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(1, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC);
        } else {
            startForeground(1, n);
        }
    }

    private synchronized void startCore() {
        if (core != null && core.isAlive()) return;

        String bin = getApplicationInfo().nativeLibraryDir + "/libagentmux.so";
        File data = new File(getFilesDir(), "agentmux");
        //noinspection ResultOfMethodCallIgnored
        data.mkdirs();

        ProcessBuilder pb = new ProcessBuilder(bin, "--addr", "127.0.0.1:" + PORT)
                .redirectErrorStream(true);
        Map<String, String> env = pb.environment();
        env.put("AGENTMUX_TOKEN", token(this));
        env.put("AGENTMUX_DATA_DIR", data.getAbsolutePath());
        env.put("HOME", getFilesDir().getAbsolutePath());
        try {
            core = pb.start();
        } catch (Exception e) {
            Log.e(TAG, "could not start the core", e);
            return;
        }
        // Drain the core's log so the pipe never fills and stalls it, and so
        // logcat shows what the core said when something goes wrong.
        Process p = core;
        new Thread(() -> {
            try (java.io.BufferedReader r = new java.io.BufferedReader(
                    new java.io.InputStreamReader(p.getInputStream()))) {
                String line;
                while ((line = r.readLine()) != null) Log.i(TAG, line);
            } catch (Exception ignored) {
            }
            Log.w(TAG, "core process ended");
        }, "agentmux-core-log").start();
    }

    @Override
    public void onDestroy() {
        if (core != null) core.destroy();
        core = null;
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}
