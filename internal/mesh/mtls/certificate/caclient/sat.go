package caclient

import (
	"os"
)

const SATLocation = "/var/run/secrets/kubernetes.io/serviceaccount/token"

func ServiceAccountToken() string {
	token, _ := os.ReadFile(SATLocation)
	return string(token)
}
