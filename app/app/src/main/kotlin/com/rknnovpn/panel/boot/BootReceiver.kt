package com.rknnovpn.panel.boot

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import com.rknnovpn.panel.ipc.DaemonClient
import com.rknnovpn.panel.ipc.DaemonctlResult
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

@AndroidEntryPoint
class BootReceiver : BroadcastReceiver() {

    @Inject
    lateinit var client: DaemonClient

    override fun onReceive(context: Context, intent: Intent) {
        val action = intent.action ?: return
        if (action !in supportedActions) return

        val pending = goAsync()
        CoroutineScope(SupervisorJob() + Dispatchers.IO).launch {
            try {
                ensureDaemonControlPlane(action)
            } finally {
                pending.finish()
            }
        }
    }

    private suspend fun ensureDaemonControlPlane(action: String) {
        val state = client.moduleState()
        if (state !is DaemonctlResult.Success) {
            Log.w(TAG, "Module state unavailable after $action: $state")
            return
        }

        val obj = state.data.jsonObject
        val daemonReady = obj["daemonAlive"]?.jsonPrimitive?.booleanOrNull == true &&
            obj["socketPresent"]?.jsonPrimitive?.booleanOrNull == true
        if (daemonReady) {
            Log.d(TAG, "Daemon control plane is already online after $action")
            return
        }

        val moduleStatus = obj["status"]?.jsonPrimitive?.contentOrNull.orEmpty()
        if (moduleStatus in nonRepairableStatuses) {
            Log.w(TAG, "Skipping boot repair after $action; module status=$moduleStatus")
            return
        }

        val repair = client.moduleRepair()
        Log.i(TAG, "Daemon control-plane repair after $action: $repair")
    }

    private companion object {
        const val TAG = "BootReceiver"

        val supportedActions = setOf(
            Intent.ACTION_BOOT_COMPLETED,
            Intent.ACTION_MY_PACKAGE_REPLACED,
        )

        val nonRepairableStatuses = setOf(
            "pending_update",
            "pending_remove",
            "disabled",
            "missing",
            "service_missing",
        )
    }
}
