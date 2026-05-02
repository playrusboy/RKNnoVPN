package com.rknnovpn.panel.ui.apps

import org.junit.Assert.assertEquals
import org.junit.Test

class AppPickerUiStateTest {
    @Test
    fun filteredAppsDoesNotMoveSelectedAppsToTop() {
        val state = AppPickerUiState(
            apps = listOf(
                AppInfo(packageName = "com.zeta", label = "Zeta", isSystemApp = false, isProxied = true),
                AppInfo(packageName = "com.alpha", label = "Alpha", isSystemApp = false, isProxied = false),
            ),
        )

        assertEquals(
            listOf("com.alpha", "com.zeta"),
            state.filteredApps.map { it.packageName },
        )
    }
}
