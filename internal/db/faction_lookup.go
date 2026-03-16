package db

import (
	"context"
	"log/slog"

	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// knownFactions is the seed data for the faction_lookup table.
// Sorted by base faction, then variants.
var knownFactions = []models.Faction{
	// Court of the Seven-Headed Serpent (no variants)
	{FactionSlug: "fc_courtofthesevenheadedserpent", BaseFaction: "fc_courtofthesevenheadedserpent", DisplayName: "Court of the Seven-Headed Serpent", IsVariant: false},

	// Cult of the Black Grail
	{FactionSlug: "fc_cultoftheblackgrail", BaseFaction: "fc_cultoftheblackgrail", DisplayName: "Cult of the Black Grail", IsVariant: false},
	{FactionSlug: "fc_cultoftheblackgrail_fv_dirgeofthegreathegemon", BaseFaction: "fc_cultoftheblackgrail", DisplayName: "Dirge of the Great Hegemon", IsVariant: true},
	{FactionSlug: "fc_cultoftheblackgrail_fv_thegreathunger", BaseFaction: "fc_cultoftheblackgrail", DisplayName: "The Great Hunger", IsVariant: true},

	// Heretic Legion
	{FactionSlug: "fc_hereticlegion", BaseFaction: "fc_hereticlegion", DisplayName: "Heretic Legion", IsVariant: false},
	{FactionSlug: "fc_hereticlegion_fv_knightsofavarice", BaseFaction: "fc_hereticlegion", DisplayName: "Knights of Avarice", IsVariant: true},
	{FactionSlug: "fc_hereticlegion_fv_navalraidingparty", BaseFaction: "fc_hereticlegion", DisplayName: "Naval Raiding Party", IsVariant: true},
	{FactionSlug: "fc_hereticlegion_fv_trenchghosts", BaseFaction: "fc_hereticlegion", DisplayName: "Trench Ghosts", IsVariant: true},

	// Iron Sultanate
	{FactionSlug: "fc_ironsultanate", BaseFaction: "fc_ironsultanate", DisplayName: "Iron Sultanate", IsVariant: false},
	{FactionSlug: "fc_ironsultanate_fv_defendersoftheironwall", BaseFaction: "fc_ironsultanate", DisplayName: "Defenders of the Iron Wall", IsVariant: true},
	{FactionSlug: "fc_ironsultanate_fv_houseofwisdom", BaseFaction: "fc_ironsultanate", DisplayName: "House of Wisdom", IsVariant: true},

	// New Antioch
	{FactionSlug: "fc_newantioch", BaseFaction: "fc_newantioch", DisplayName: "New Antioch", IsVariant: false},
	{FactionSlug: "fc_newantioch_fv_alba", BaseFaction: "fc_newantioch", DisplayName: "Alba", IsVariant: true},
	{FactionSlug: "fc_newantioch_fv_abyssinia", BaseFaction: "fc_newantioch", DisplayName: "Abyssinia", IsVariant: true},
	{FactionSlug: "fc_newantioch_fv_papalstates", BaseFaction: "fc_newantioch", DisplayName: "Papal States", IsVariant: true},
	{FactionSlug: "fc_newantioch_fv_redbrigade", BaseFaction: "fc_newantioch", DisplayName: "Red Brigade", IsVariant: true},
	// These two use _fc_ instead of _fv_ — inconsistent upstream encoding
	{FactionSlug: "fc_newantioch_fc_eirerangers", BaseFaction: "fc_newantioch", DisplayName: "Eire Rangers", IsVariant: true},
	{FactionSlug: "fc_newantioch_fc_stortruppenofthefreestateofprussia", BaseFaction: "fc_newantioch", DisplayName: "Stortruppen of the Free State of Prussia", IsVariant: true},

	// Trench Pilgrim
	{FactionSlug: "fc_trenchpilgrim", BaseFaction: "fc_trenchpilgrim", DisplayName: "Trench Pilgrim", IsVariant: false},
	{FactionSlug: "fc_trenchpilgrim_fv_tenthplague", BaseFaction: "fc_trenchpilgrim", DisplayName: "Tenth Plague", IsVariant: true},
	{FactionSlug: "fc_trenchpilgrim_fv_sacredaffliction", BaseFaction: "fc_trenchpilgrim", DisplayName: "Sacred Affliction", IsVariant: true},
	{FactionSlug: "fc_trenchpilgrim_fv_saintmethodius", BaseFaction: "fc_trenchpilgrim", DisplayName: "Saint Methodius", IsVariant: true},
}

// knownBaseFactions is the list of base faction slugs sorted by length descending
// for the faction parsing algorithm.
var knownBaseFactions = []string{
	"fc_courtofthesevenheadedserpent", // 31 chars
	"fc_cultoftheblackgrail",          // 22 chars
	"fc_hereticlegion",                // 16 chars
	"fc_ironsultanate",                // 16 chars
	"fc_trenchpilgrim",                // 16 chars
	"fc_newantioch",                   // 13 chars
}

// ParseFaction parses a faction slug into base faction and variant.
// Returns the full slug as base if the faction is unknown.
func ParseFaction(slug string) (base, variant string) {
	for _, b := range knownBaseFactions {
		if slug == b {
			return b, ""
		}
		if len(slug) > len(b) && slug[:len(b)+1] == b+"_" {
			return b, slug[len(b)+1:]
		}
	}
	slog.Warn("unknown faction slug", "slug", slug)
	return slug, ""
}

// seedFactions inserts or updates the faction_lookup table with known factions.
func (d *DB) seedFactions(ctx context.Context) error {
	for _, f := range knownFactions {
		_, err := d.pool.ExecContext(ctx,
			`INSERT OR REPLACE INTO faction_lookup (faction_slug, base_faction, display_name, is_variant)
			 VALUES ($1, $2, $3, $4)`,
			f.FactionSlug, f.BaseFaction, f.DisplayName, f.IsVariant,
		)
		if err != nil {
			return err
		}
	}
	slog.Info("seeded faction lookup", "count", len(knownFactions))
	return nil
}
