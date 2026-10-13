package knox

// IsEnrolled reports whether a Knox device status means the device is enrolled: provisioned (P) and
// deactivated (I) devices are not, blocked ones still are, and unknown codes are not.
func IsEnrolled(status string) bool {
	switch status {
	case "A", "B", "BS", "BA", "BL":
		return true
	}
	return false
}
