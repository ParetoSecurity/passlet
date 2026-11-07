package passlet

import (
	"os"
)

func ResolveEndpoint(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if endpoint := os.Getenv("PASSLET_ENDPOINT"); endpoint != "" {
		return endpoint, nil
	}
	c, err := readConnection()
	if os.IsNotExist(err) {
		return "unix://" + DefaultSocket(), nil
	}
	if err != nil {
		return "", err
	}
	return c.Endpoint, nil
}
