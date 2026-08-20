package com.agentmux.app;

import android.content.Intent;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

/**
 * The one native call the connect page needs: start the embedded core and
 * say where it listens and how to authenticate. Its absence (in a plain
 * browser) is what tells the page to offer remote-serve mode instead.
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
}
