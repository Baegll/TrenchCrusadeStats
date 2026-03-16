package db

import "testing"

func TestParseFaction(t *testing.T) {
	tests := []struct {
		slug        string
		wantBase    string
		wantVariant string
	}{
		{"fc_newantioch", "fc_newantioch", ""},
		{"fc_hereticlegion", "fc_hereticlegion", ""},
		{"fc_trenchpilgrim", "fc_trenchpilgrim", ""},
		{"fc_cultoftheblackgrail", "fc_cultoftheblackgrail", ""},
		{"fc_courtofthesevenheadedserpent", "fc_courtofthesevenheadedserpent", ""},
		{"fc_ironsultanate", "fc_ironsultanate", ""},
		// Variants with _fv_
		{"fc_newantioch_fv_redbrigade", "fc_newantioch", "fv_redbrigade"},
		{"fc_hereticlegion_fv_trenchghosts", "fc_hereticlegion", "fv_trenchghosts"},
		{"fc_cultoftheblackgrail_fv_thegreathunger", "fc_cultoftheblackgrail", "fv_thegreathunger"},
		// Variants with _fc_ (inconsistent upstream)
		{"fc_newantioch_fc_eirerangers", "fc_newantioch", "fc_eirerangers"},
		{"fc_newantioch_fc_stortruppenofthefreestateofprussia", "fc_newantioch", "fc_stortruppenofthefreestateofprussia"},
		// Unknown
		{"fc_unknownfaction", "fc_unknownfaction", ""},
		{"", "", ""},
		// Prefix collision safety
		{"fc_newantioch_extra", "fc_newantioch", "extra"},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			base, variant := ParseFaction(tt.slug)
			if base != tt.wantBase {
				t.Errorf("ParseFaction(%q) base = %q, want %q", tt.slug, base, tt.wantBase)
			}
			if variant != tt.wantVariant {
				t.Errorf("ParseFaction(%q) variant = %q, want %q", tt.slug, variant, tt.wantVariant)
			}
		})
	}
}
