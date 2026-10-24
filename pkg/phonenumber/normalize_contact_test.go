package phonenumber

import "testing"

// Imported numbers end up in the form users.phone keeps, whatever way the
// phone book wrote them (task 011): the login's form for international
// numbers, the importer's country for national ones.
func TestNormalizeContact(t *testing.T) {
	for _, c := range []struct {
		raw, region, want string
	}{
		// International, with or without '+' and formatting: one number.
		{"+7 (999) 000-00-01", "", "79990000001"},
		{"79990000001", "", "79990000001"},
		{"+79990000001", "US", "79990000001"},
		{"79990000001", "US", "79990000001"}, // not a US national number
		{"  +1 201-555-0100 ", "RU", "12015550100"},
		// National numbers of the importer's country.
		{"8 (999) 000-00-01", "RU", "79990000001"},
		{"89990000001", "RU", "79990000001"},
		{"9990000001", "RU", "79990000001"},
		{"79990000001", "RU", "79990000001"},
		{"(201) 555-0100", "US", "12015550100"},
		{"2015550100", "US", "12015550100"},
		// International numbers written without '+' that are not national.
		{"4915112345678", "RU", "4915112345678"},
		{"12015550100", "RU", "12015550100"},
	} {
		got, err := NormalizeContact(c.raw, c.region)
		if err != nil || got != c.want {
			t.Errorf("NormalizeContact(%q, %q) = %q, %v; want %q", c.raw, c.region, got, err, c.want)
		}
	}
}

func TestNormalizeContactRejectsWhatIsNotANumber(t *testing.T) {
	for _, c := range []struct{ raw, region string }{
		{"", "RU"},
		{"   ", ""},
		{"abc", "US"},
		{"123", "RU"},
		{"+7 999", ""},
		{"8 (999) 000-00-01", ""}, // national, but whose?
	} {
		if got, err := NormalizeContact(c.raw, c.region); err == nil {
			t.Errorf("NormalizeContact(%q, %q) = %q, want an error", c.raw, c.region, got)
		}
	}
}
