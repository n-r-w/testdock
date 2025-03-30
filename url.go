package testdock

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// dbURL represents a parsed database connection string.
// Supported connection string format:
// [protocol://]user:password@[transport(]host:port[)][/database][?option1=a&option2=b]
//
// Required fields: user, password, host, port
// Optional fields: protocol, transport, database and options.
type dbURL struct {
	Protocol  string
	Transport string
	User      string
	Password  string
	Host      string
	Port      int
	Database  string
	Options   map[string]string // option1=a&option2=b -> {"option1": "a", "option2": "b"}
}

// parseURL parses a connection string into a URL.
func parseURL(connStr string) (*dbURL, error) {
	if connStr == "" {
		return nil, errors.New("connection string cannot be empty")
	}

	u := &dbURL{
		Options: make(map[string]string),
	}

	const splitCount = 2

	var rest string

	// Split protocol and the rest
	parts := strings.SplitN(connStr, "://", splitCount)
	if len(parts) == splitCount {
		// Parse protocol
		u.Protocol = parts[0]
		if u.Protocol == "" {
			return nil, errors.New("invalid connection string format: '://' exists, but no protocol")
		}

		rest = parts[1]
	} else {
		rest = connStr
	}

	// Find the last @ to properly handle @ in passwords
	atIndex := strings.LastIndex(rest, "@")
	if atIndex >= 0 {
		credentials := rest[:atIndex]
		rest = rest[atIndex+1:]

		// Parse credentials
		credParts := strings.SplitN(credentials, ":", splitCount)
		if len(credParts) != splitCount {
			return nil, errors.New("invalid connection string format: missing password")
		}
		u.User = credParts[0]
		if u.User == "" {
			return nil, errors.New("user is required")
		}
		u.Password = credParts[1]
		if u.Password == "" {
			return nil, errors.New("password is required")
		}
	}

	// Split query parameters if they exist
	hostAndQuery := strings.SplitN(rest, "?", splitCount)
	rest = hostAndQuery[0]

	// Parse query parameters if they exist
	if len(hostAndQuery) > 1 {
		queryStr := hostAndQuery[1]
		for _, param := range strings.Split(queryStr, "&") {
			kv := strings.SplitN(param, "=", splitCount)
			if len(kv) == splitCount {
				u.Options[kv[0]] = kv[1]
			}
		}
	}

	// Parse database if exists
	hostAndDB := strings.SplitN(rest, "/", splitCount)
	rest = hostAndDB[0]
	if len(hostAndDB) > 1 {
		u.Database = hostAndDB[1]
	}

	// Check if transport is specified
	if strings.Contains(rest, "(") && strings.HasSuffix(rest, ")") {
		transportParts := strings.SplitN(rest, "(", splitCount)
		if len(transportParts) != splitCount {
			return nil, errors.New("invalid connection string format: malformed transport")
		}
		u.Transport = transportParts[0]
		rest = strings.TrimSuffix(transportParts[1], ")")
	}

	if rest == "" {
		return nil, errors.New("host is required")
	}

	// Parse host and port
	hostAndPort := strings.SplitN(rest, ":", splitCount)
	if len(hostAndPort) != splitCount {
		return nil, errors.New("invalid connection string format: missing port")
	}
	u.Host = hostAndPort[0]
	if u.Host == "" {
		return nil, errors.New("host is required")
	}

	if hostAndPort[1] == "" {
		return nil, errors.New("port is required")
	}
	p, err := strconv.Atoi(hostAndPort[1])
	if err != nil {
		return nil, fmt.Errorf("parse port: %w", err)
	}
	if p <= 0 {
		return nil, errors.New("port must be positive")
	}
	u.Port = p

	return u, nil
}

// string returns the connection string representation of the URL.
func (u *dbURL) string(hidePassword bool) string {
	if u == nil {
		return ""
	}

	var b strings.Builder

	// Write protocol
	if u.Protocol != "" {
		b.WriteString(u.Protocol)
		b.WriteString("://")
	}

	if u.User != "" {
		// Write credentials
		b.WriteString(u.User)
		b.WriteString(":")
		if hidePassword {
			b.WriteString("*****")
		} else {
			b.WriteString(u.Password)
		}
		b.WriteString("@")
	}

	// Write transport, host and port
	if u.Transport != "" {
		b.WriteString(u.Transport)
		b.WriteString("(")
	}
	b.WriteString(u.Host)
	if u.Port != 0 {
		b.WriteString(":" + strconv.Itoa(u.Port))
	}
	if u.Transport != "" {
		b.WriteString(")")
	}

	// Write database if exists
	if u.Database != "" {
		b.WriteString("/" + u.Database)
	}

	// Write options if exist
	if len(u.Options) > 0 {
		b.WriteString("?")

		// Sort keys for deterministic output
		keys := make([]string, 0, len(u.Options))
		for k := range u.Options {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for i, k := range keys {
			if i > 0 {
				b.WriteString("&")
			}
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(u.Options[k])
		}
	}

	return b.String()
}

// clone returns a copy of the URL.
func (u *dbURL) clone() *dbURL {
	if u == nil {
		return nil
	}

	clone := &dbURL{
		Protocol:  u.Protocol,
		Transport: u.Transport,
		User:      u.User,
		Password:  u.Password,
		Host:      u.Host,
		Port:      u.Port,
		Database:  u.Database,
		Options:   make(map[string]string, len(u.Options)),
	}

	// Deep copy the options map
	for k, v := range u.Options {
		clone.Options[k] = v
	}

	return clone
}

// replaceDatabase replaces the database name in the URL.
func (u *dbURL) replaceDatabase(newDBName string) *dbURL {
	clone := u.clone()
	clone.Database = newDBName
	return clone
}
