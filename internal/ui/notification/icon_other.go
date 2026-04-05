//go:build !darwin

package notification

import (
	_ "embed"
)

//go:embed lenos-icon-solo.png
var icon []byte

// Icon contains the embedded PNG icon data for desktop notifications.
var Icon any = icon
