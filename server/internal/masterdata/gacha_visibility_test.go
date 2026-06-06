package masterdata

import (
	"testing"

	"lunar-tear/server/internal/model"
	"lunar-tear/server/internal/store"
)

// TestBuildGachaEntriesMedalOptional verifies that banners without a
// m_gacha_medal row are no longer dropped from the catalog, while banners
// that do have a medal keep their pity/medal fields.
func TestBuildGachaEntriesMedalOptional(t *testing.T) {
	banners := []EntityMMomBanner{
		{DestinationDomainType: model.MomBannerDomainGacha, DestinationDomainId: 100, BannerAssetName: "limited_100"}, // no medal
		{DestinationDomainType: model.MomBannerDomainGacha, DestinationDomainId: 200, BannerAssetName: "premium_200"}, // has medal
		{DestinationDomainType: model.MomBannerDomainGacha, DestinationDomainId: 300, BannerAssetName: "common_300"},  // chapter, no medal
		{DestinationDomainType: model.MomBannerDomainGacha, DestinationDomainId: 400, BannerAssetName: "step_up_400"}, // stepup, no medal
		{DestinationDomainType: 21, DestinationDomainId: 500, BannerAssetName: ""},                                    // non-gacha domain -> skipped
	}
	gachaToMedal := map[int32]EntityMGachaMedal{
		200: {GachaMedalId: 8200, ConsumableItemId: 8200, ShopTransitionGachaId: 200},
	}

	byId := map[int32]store.GachaCatalogEntry{}
	for _, e := range buildGachaEntries(banners, gachaToMedal) {
		byId[e.GachaId] = e
	}

	// Regression: limited banner without a medal row is now present.
	lim, ok := byId[100]
	if !ok {
		t.Fatal("limited_ banner without a medal row was dropped (regression)")
	}
	if lim.GachaMedalId != 0 || lim.MedalConsumableItemId != 0 || lim.CeilingCount != 0 {
		t.Errorf("no-medal banner should have zero medal fields, got %+v", lim)
	}
	if lim.GachaLabelType != model.GachaLabelPremium {
		t.Errorf("limited banner label = %d, want premium", lim.GachaLabelType)
	}

	// Premium banner with a medal keeps its medal + ceiling.
	if prem := byId[200]; prem.GachaMedalId != 8200 || prem.MedalConsumableItemId != 8200 || prem.CeilingCount != model.PityCeilingCount {
		t.Errorf("medal banner lost its medal/ceiling: %+v", prem)
	}

	// Chapter banner present and labeled chapter.
	if ch, ok := byId[300]; !ok || ch.GachaLabelType != model.GachaLabelChapter {
		t.Errorf("chapter banner missing/mislabeled: %+v ok=%v", ch, ok)
	}

	// Regression: step-up banner without a medal is present, with no pity ceiling.
	su, ok := byId[400]
	if !ok {
		t.Fatal("step_up_ banner without a medal row was dropped (regression)")
	}
	if su.GachaModeType != model.GachaModeStepup || su.CeilingCount != 0 {
		t.Errorf("stepup banner wrong: mode=%d ceiling=%d", su.GachaModeType, su.CeilingCount)
	}

	// Non-gacha-domain banner is skipped.
	if _, ok := byId[500]; ok {
		t.Error("non-gacha-domain banner should be skipped")
	}
}
