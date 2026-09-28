package fmtparse

// customType is the internal record for a field type registered with WithType.
type customType struct {
	pattern    string
	groupCount int
	convert    func(string) (any, error)
}

// config accumulates the options passed to Parse, Search, FindAll, and Compile.
type config struct {
	caseSensitive bool
	extraTypes    map[string]customType
}

// Option configures parsing behaviour. See [CaseSensitive] and [WithType].
type Option func(*config)

// CaseSensitive makes matching case-sensitive. fmtparse matches
// case-insensitively by default, mirroring Python's parse library.
func CaseSensitive() Option {
	return func(c *config) {
		c.caseSensitive = true
	}
}

// WithType registers a custom field type. pattern is a regular expression
// (RE2 syntax) that matches the raw text for the field; convert turns the
// matched text into a Go value. Use it as {name:type} in a format string,
// where type is the name passed here.
//
// pattern must not contain capturing groups; use non-capturing groups
// ((?:...)) if grouping is required.
func WithType(name, pattern string, convert func(string) (any, error)) Option {
	return func(c *config) {
		if c.extraTypes == nil {
			c.extraTypes = map[string]customType{}
		}
		c.extraTypes[name] = customType{pattern: pattern, convert: convert}
	}
}

func newConfig(opts []Option) *config {
	c := &config{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}
