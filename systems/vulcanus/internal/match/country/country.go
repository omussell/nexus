// Package country loads country data from a text file and provides code/region mapping.
package country

import (
	"bufio"
	"os"
	"strings"
)

// Country represents a country with its ISO code and normalized name.
type Country struct {
	Code string
	Name string
}

// Load reads a countries.txt file (format: "code name") and returns all countries.
func Load(path string) ([]Country, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var countries []Country
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		countries = append(countries, Country{
			Code: strings.ToUpper(parts[0]),
			Name: strings.ToLower(parts[1]),
		})
	}
	return countries, scanner.Err()
}

// ToRegion converts a country code to its region.
// GB -> GB-UK, UK -> GB-UK, CN -> CN-HK-TW, HK -> CN-HK-TW, TW -> CN-HK-TW, US -> US-PR, PR -> US-PR, CA -> CA
func ToRegion(code string) string {
	code = strings.ToUpper(code)
	switch code {
	case "GB", "UK":
		return "GB-UK"
	case "CN", "HK", "TW":
		return "CN-HK-TW"
	case "US", "PR":
		return "US-PR"
	case "CA":
		return "CA"
	case "JP", "KR", "SG", "AU", "NZ", "TH", "MY", "PH", "ID", "VN", "IN":
		return "APSC"
	default:
		return code
	}
}

// IsInRegion checks if a country code maps to the given region.
func IsInRegion(code, region string) bool {
	return ToRegion(code) == region
}
