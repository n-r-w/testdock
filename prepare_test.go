package testdock

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func checkInformer(t *testing.T, defaultDSN string, informer Informer) {
	t.Helper()

	defaultURL, err := parseURL(defaultDSN)
	require.NoError(t, err)

	url, err := parseURL(informer.DSN())
	require.NoError(t, err)

	require.NotEqual(t, defaultURL.Database, url.Database)
	require.NotEqual(t, defaultURL.Database, informer.DatabaseName())
	require.Equal(t, defaultURL.User, url.User)
	require.Equal(t, defaultURL.Password, url.Password)
	require.Equal(t, defaultURL.Host, informer.Host())
	require.Equal(t, url.Port, informer.Port())
}
