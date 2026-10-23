package campaign

import (
	"strings"
	"testing"
)

func TestNewCampaignValidate(t *testing.T) {
	valid := func() NewCampaign {
		return NewCampaign{Title: "The Last Frame", Category: "drama", Goal: MinGoalPaise}
	}

	ok := valid()
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid campaign rejected: %v", err)
	}
	if ok.Category != "DRAMA" {
		t.Errorf("category not normalised: %q", ok.Category)
	}

	cases := map[string]func(*NewCampaign){
		"short title":       func(n *NewCampaign) { n.Title = "ab" },
		"long title":        func(n *NewCampaign) { n.Title = strings.Repeat("x", 121) },
		"long tagline":      func(n *NewCampaign) { n.Tagline = strings.Repeat("x", 201) },
		"bad category":      func(n *NewCampaign) { n.Category = "WESTERN" },
		"goal too small":    func(n *NewCampaign) { n.Goal = MinGoalPaise - 1 },
		"javascript url":    func(n *NewCampaign) { n.CoverKey = "javascript:alert(1)" },
		"http url":          func(n *NewCampaign) { n.CoverKey = "http://example.com/a.jpg" },
		"protocol-relative": func(n *NewCampaign) { n.CoverKey = "//evil.example/a.jpg" },
	}
	for name, mutate := range cases {
		n := valid()
		mutate(&n)
		if err := n.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}

	withCover := valid()
	withCover.CoverKey = "https://images.unsplash.com/photo-1?w=1200"
	if err := withCover.Validate(); err != nil {
		t.Errorf("https cover rejected: %v", err)
	}
}

func TestNewTierValidate(t *testing.T) {
	zero := 0
	cases := map[string]NewTier{
		"short title": {Title: "x", MinAmount: MinTierPaise},
		"low amount":  {Title: "Poster", MinAmount: MinTierPaise - 1},
		"zero limit":  {Title: "Poster", MinAmount: MinTierPaise, QuantityLimit: &zero},
	}
	for name, tier := range cases {
		if err := tier.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	ok := NewTier{Title: "Poster + credit", MinAmount: MinTierPaise}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid tier rejected: %v", err)
	}
}
