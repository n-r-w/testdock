package testdock

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURL_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		connStr string
		want    *dbURL
		wantErr string
	}{
		{
			name:    "empty string",
			connStr: "",
			wantErr: "connection string cannot be empty",
		},
		{
			name:    "invalid format - :// exists, but no protocol",
			connStr: "://postgresuser:pass@localhost",
			wantErr: "invalid connection string format: '://' exists, but no protocol",
		},
		{
			name:    "invalid format - missing password",
			connStr: "postgres://user@localhost",
			wantErr: "invalid connection string format: missing password",
		},
		{
			name:    "missing user",
			connStr: "postgres://:pass@localhost",
			wantErr: "user is required",
		},
		{
			name:    "missing password",
			connStr: "postgres://user:@localhost",
			wantErr: "password is required",
		},
		{
			name:    "missing host",
			connStr: "postgres://user:pass@",
			wantErr: "host is required",
		},
		{
			name:    "missing port",
			connStr: "postgres://user:pass@localhost:",
			wantErr: "port is required",
		},
		{
			name:    "minimal valid URL",
			connStr: "localhost:5432",
			want: &dbURL{
				Host:    "localhost",
				Port:    5432,
				Options: make(map[string]string),
			},
		},
		{
			name:    "minimal valid URL with user and password",
			connStr: "user:pass@localhost:5432",
			want: &dbURL{
				User:     "user",
				Password: "pass",
				Host:     "localhost",
				Port:     5432,
				Options:  make(map[string]string),
			},
		},
		{
			name:    "no user and password",
			connStr: "mongodb://localhost:27017/testdb?directConnection=true",
			want: &dbURL{
				Protocol: "mongodb",
				Host:     "localhost",
				Port:     27017,
				Database: "testdb",
				Options: map[string]string{
					"directConnection": "true",
				},
			},
		},
		{
			name:    "full URL with all optional fields",
			connStr: "mysql://root:secret@tcp(127.0.0.1:3306)/testdb?charset=utf8&opt2=val2",
			want: &dbURL{
				Protocol:  "mysql",
				Transport: "tcp",
				User:      "root",
				Password:  "secret",
				Host:      "127.0.0.1",
				Port:      3306,
				Database:  "testdb",
				Options: map[string]string{
					"charset": "utf8",
					"opt2":    "val2",
				},
			},
		},
		{
			name:    "URL with special characters in password",
			connStr: `postgres://user:p@ss/\:!w0rd@localhost:5432/mydb`,
			want: &dbURL{
				Protocol: "postgres",
				User:     "user",
				Password: `p@ss/\:!w0rd`,
				Host:     "localhost",
				Port:     5432,
				Database: "mydb",
				Options:  make(map[string]string),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURL(tt.connStr)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestURL_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  *dbURL
		want string
	}{
		{
			name: "nil URL",
			url:  nil,
			want: "",
		},
		{
			name: "minimal URL",
			url: &dbURL{
				Protocol: "postgres",
				User:     "user",
				Password: "pass",
				Host:     "localhost",
				Port:     5432,
				Options:  make(map[string]string),
			},
			want: "postgres://user:pass@localhost:5432",
		},
		{
			name: "full URL",
			url: &dbURL{
				Protocol:  "mysql",
				Transport: "tcp",
				User:      "root",
				Password:  "secret",
				Host:      "127.0.0.1",
				Port:      3306,
				Database:  "testdb",
				Options: map[string]string{
					"charset": "utf8",
					"opt2":    "val2",
				},
			},
			want: "mysql://root:secret@tcp(127.0.0.1:3306)/testdb?charset=utf8&opt2=val2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.url.string(false)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestURL_Clone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  *dbURL
	}{
		{
			name: "nil URL",
			url:  nil,
		},
		{
			name: "empty URL",
			url: &dbURL{
				Options: make(map[string]string),
			},
		},
		{
			name: "full URL",
			url: &dbURL{
				Protocol:  "postgres",
				Transport: "ssl",
				User:      "user",
				Password:  "pass",
				Host:      "localhost",
				Port:      5432,
				Database:  "mydb",
				Options: map[string]string{
					"sslmode": "verify-full",
					"timeout": "30",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clone := tt.url.clone()

			if tt.url == nil {
				assert.Nil(t, clone)
				return
			}

			// Check that all fields are equal
			assert.Equal(t, tt.url, clone)

			// Check that it's a different pointer
			assert.False(t, tt.url == clone, "Clone should return a new pointer")

			// Verify deep copy of Options map
			assert.Equal(t, tt.url.Options, clone.Options)

			// Modify clone's Options to verify it doesn't affect original
			if len(clone.Options) > 0 {
				origValue := tt.url.Options["sslmode"]
				clone.Options["sslmode"] = "modified"
				assert.Equal(t, origValue, tt.url.Options["sslmode"], "Modifying clone's Options should not affect original")
				assert.NotEqual(t, tt.url.Options["sslmode"], clone.Options["sslmode"], "Clone's Options should be modifiable independently")
			}
		})
	}
}

// TestParse_RoundTrip tests that parsing a URL and converting it back to string
// results in an equivalent URL.
func TestParse_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []string{
		"postgres://user:pass@localhost:5432",
		"postgres://user:pass@ssl(localhost:5432)/mydb?sslmode=verify-full&timeout=30",
		"mysql://root:secret@tcp(127.0.0.1:3306)/testdb?charset=utf8",
	}

	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			parsed, err := parseURL(url)
			require.NoError(t, err)

			got := parsed.string(false)
			assert.Equal(t, url, got)
		})
	}
}
