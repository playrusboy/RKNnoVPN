package com.rknnovpn.panel.ui.navigation

import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow

@Composable
fun BottomNavBar(
    currentRoute: String?,
    onNavigate: (TopLevelRoute) -> Unit,
) {
    NavigationBar {
        TopLevelRoute.entries.forEach { destination ->
            val selected = currentRoute == destination.route
            NavigationBarItem(
                selected = selected,
                onClick = { onNavigate(destination) },
                icon = {
                    Icon(
                        imageVector = if (selected) destination.selectedIcon
                        else destination.unselectedIcon,
                        contentDescription = stringResource(destination.labelRes),
                    )
                },
                label = {
                    Text(
                        text = stringResource(destination.labelRes),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                },
            )
        }
    }
}
