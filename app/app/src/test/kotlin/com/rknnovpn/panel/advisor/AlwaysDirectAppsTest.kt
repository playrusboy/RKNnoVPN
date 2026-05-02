package com.rknnovpn.panel.advisor

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AlwaysDirectAppsTest {
    @Test
    fun clashRoyaleIsNotAlwaysDirectBecauseOfKeyword() {
        assertFalse(AlwaysDirectApps.matches("com.supercell.clashroyale"))
    }

    @Test
    fun userExclusionOverridesManualAndBuiltInDirect() {
        assertFalse(
            AlwaysDirectApps.matches(
                packageName = "ru.sberbankmobile",
                manualPackages = setOf("ru.sberbankmobile"),
                excludedPackages = setOf("ru.sberbankmobile"),
            ),
        )
    }

    @Test
    fun knownRussianBankRemainsAlwaysDirect() {
        assertTrue(AlwaysDirectApps.matches("ru.sberbankmobile"))
    }
}
