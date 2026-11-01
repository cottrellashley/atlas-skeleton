package protocol

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Identifier limits (§9.1.3).
const (
	MaxIDLength      = 128
	MaxAddressLength = 160
)

var (
	idPattern          = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)
	segmentPattern     = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// ValidID reports whether id is a capability id: lowercase dot-separated
// segments of letters, digits and hyphens (ATLAS-CAP-002, §9.1.3).
func ValidID(id string) error {
	if len(id) > MaxIDLength {
		return fmt.Errorf("capability id %q is longer than %d characters", id, MaxIDLength)
	}
	if !idPattern.MatchString(id) {
		return fmt.Errorf("capability id %q must be lowercase, dot-separated segments, such as portfolio.holdings", id)
	}
	for _, seg := range strings.Split(id, ".") {
		if strings.HasSuffix(seg, "-") || strings.Contains(seg, "--") {
			return fmt.Errorf("capability id %q has a segment that ends in '-' or contains '--'", id)
		}
	}
	return nil
}

// Domain returns the first segment of a capability id.
func Domain(id string) string {
	d, _, _ := strings.Cut(id, ".")
	return d
}

// IsBuiltin reports whether id names a built-in capability: only those have
// the domain atlas (§2.1.F).
func IsBuiltin(id string) bool { return Domain(id) == "atlas" }

// Pinned is a capability at one major version: portfolio.holdings@v1.
type Pinned struct {
	ID      string
	Version int
}

func (p Pinned) String() string { return p.ID + "@v" + strconv.Itoa(p.Version) }

// ParsePinned reads id@vN.
func ParsePinned(s string) (Pinned, error) {
	id, v, ok := strings.Cut(s, "@v")
	if !ok {
		return Pinned{}, fmt.Errorf("%q is not a pinned id (id@vN)", s)
	}
	if err := ValidID(id); err != nil {
		return Pinned{}, err
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || strconv.Itoa(n) != v {
		return Pinned{}, fmt.Errorf("%q does not end in a positive version", s)
	}
	return Pinned{ID: id, Version: n}, nil
}

// Exact names one exact contract: portfolio.holdings@v1#bcaa5bbad97cc612.
type Exact struct {
	Pinned
	Fingerprint string
}

func (e Exact) String() string { return e.Pinned.String() + "#" + e.Fingerprint }

// ParseExact reads id@vN#fingerprint.
func ParseExact(s string) (Exact, error) {
	pinned, fp, ok := strings.Cut(s, "#")
	if !ok {
		return Exact{}, fmt.Errorf("%q is not an exact id (id@vN#fingerprint)", s)
	}
	p, err := ParsePinned(pinned)
	if err != nil {
		return Exact{}, err
	}
	if !fingerprintPattern.MatchString(fp) {
		return Exact{}, fmt.Errorf("%q has no 16-character lowercase hexadecimal fingerprint", s)
	}
	return Exact{Pinned: p, Fingerprint: fp}, nil
}

// Address is what every call resolves to: id@vN:interaction (§2.1.A).
type Address struct {
	Pinned
	Interaction string
}

func (a Address) String() string { return a.Pinned.String() + ":" + a.Interaction }

// ParseAddress reads id@vN:interaction.
func ParseAddress(s string) (Address, error) {
	if len(s) > MaxAddressLength {
		return Address{}, fmt.Errorf("address is longer than %d characters", MaxAddressLength)
	}
	pinned, interaction, ok := strings.Cut(s, ":")
	if !ok {
		return Address{}, fmt.Errorf("%q is not an address (id@vN:interaction)", s)
	}
	p, err := ParsePinned(pinned)
	if err != nil {
		return Address{}, err
	}
	if !segmentPattern.MatchString(interaction) {
		return Address{}, fmt.Errorf("%q has no valid interaction", s)
	}
	return Address{Pinned: p, Interaction: interaction}, nil
}
