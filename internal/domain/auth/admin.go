package auth

func IsAdmin(email string, adminEmails []string) bool {
	for _, e := range adminEmails {
		if e == email {
			return true
		}
	}
	return false
}
