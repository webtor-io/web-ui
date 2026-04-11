package recommendations

// Static chip sets for cold-start users — those with no watch history.
//
// Why: a user who has never watched anything gives Claude zero personalisation
// signal, so calling the model would burn tokens for a generic prompt that we
// can hand-write once and serve forever. The trade-off is missing the chance
// to delight a brand-new user with something witty, but spending a free-tier
// user's only daily quota unit before they've even clicked anything is worse.
//
// The labels are localised; the queries that get sent to Claude on click are
// kept in English because Claude is told the user's locale via the prompt
// context block, so it will write reasons in the right language regardless
// of the input query language. Keeping queries in English avoids accidental
// vocabulary drift between locales.
//
// Each set contains 6 entries — the same count Claude is asked to produce —
// chosen for genre and mood diversity (mirrors the AI prompt diversity rules).

// defaultChipDef is the static spec for a chip in defaultChipsRU/EN.
type defaultChipDef struct {
	Label string
	Icon  string
	Query string
}

var defaultChipDefsEN = []defaultChipDef{
	{
		Label: "Award winners of the last decade",
		Icon:  "🏆",
		Query: "Recommend critically acclaimed films from the last decade that won major awards (Oscars, Cannes, BAFTA, Golden Globes). Mix genres.",
	},
	{
		Label: "Mind-bending sci-fi",
		Icon:  "🌌",
		Query: "Recommend mind-bending science fiction films with non-linear time, philosophical themes, or reality-warping concepts.",
	},
	{
		Label: "Slow-burn thrillers",
		Icon:  "🥶",
		Query: "Recommend slow-burn psychological thrillers that build tension gradually rather than rely on action set pieces.",
	},
	{
		Label: "Foreign-language gems",
		Icon:  "🌍",
		Query: "Recommend acclaimed non-English films from around the world — Korean, Iranian, French, Japanese, Spanish, etc. Avoid the obvious mainstream picks.",
	},
	{
		Label: "Pre-1990 classics",
		Icon:  "📽️",
		Query: "Recommend essential film classics released before 1990 that hold up brilliantly today and would surprise a modern viewer.",
	},
	{
		Label: "Films that ruined a first date",
		Icon:  "💀",
		Query: "Recommend deeply weird, uncomfortable, or unhinged films that would absolutely ruin a first date — the kind that leave you staring at the wall afterwards.",
	},
}

var defaultChipDefsRU = []defaultChipDef{
	{
		Label: "Лауреаты главных премий",
		Icon:  "🏆",
		Query: "Recommend critically acclaimed films from the last decade that won major awards (Oscars, Cannes, BAFTA, Golden Globes). Mix genres.",
	},
	{
		Label: "Сай-фай, выносящий мозг",
		Icon:  "🌌",
		Query: "Recommend mind-bending science fiction films with non-linear time, philosophical themes, or reality-warping concepts.",
	},
	{
		Label: "Медленные триллеры",
		Icon:  "🥶",
		Query: "Recommend slow-burn psychological thrillers that build tension gradually rather than rely on action set pieces.",
	},
	{
		Label: "Шедевры на других языках",
		Icon:  "🌍",
		Query: "Recommend acclaimed non-English films from around the world — Korean, Iranian, French, Japanese, Spanish, etc. Avoid the obvious mainstream picks.",
	},
	{
		Label: "Классика до 1990 года",
		Icon:  "📽️",
		Query: "Recommend essential film classics released before 1990 that hold up brilliantly today and would surprise a modern viewer.",
	},
	{
		Label: "Фильмы, которые угробят первое свидание",
		Icon:  "💀",
		Query: "Recommend deeply weird, uncomfortable, or unhinged films that would absolutely ruin a first date — the kind that leave you staring at the wall afterwards.",
	},
}

// defaultChips returns the static chip set for the given locale, with stable
// chip IDs derived from the labels.
func defaultChips(locale string) []Chip {
	defs := defaultChipDefsEN
	if locale == "ru" {
		defs = defaultChipDefsRU
	}
	out := make([]Chip, len(defs))
	for i, d := range defs {
		out[i] = Chip{
			ID:    shortHash(d.Label),
			Label: d.Label,
			Icon:  d.Icon,
			Query: d.Query,
		}
	}
	return out
}
