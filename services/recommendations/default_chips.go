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
//
// To add a new locale: register it in services/recommendations/context.go
// supportedLocales, add a defaultChipDefs<XX> slice here, and add a case to
// defaultChipsByLocale. Labels translate the EN concepts; queries are copied
// verbatim — Claude reads the locale from UserContext and writes reasons in
// the right language regardless of query language.

// defaultChipDef is the static spec for a chip in the per-locale defaultChipDefs* slices.
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

var defaultChipDefsES = []defaultChipDef{
	{
		Label: "Premiadas de la última década",
		Icon:  "🏆",
		Query: "Recommend critically acclaimed films from the last decade that won major awards (Oscars, Cannes, BAFTA, Golden Globes). Mix genres.",
	},
	{
		Label: "Sci-fi que te vuela la cabeza",
		Icon:  "🌌",
		Query: "Recommend mind-bending science fiction films with non-linear time, philosophical themes, or reality-warping concepts.",
	},
	{
		Label: "Thrillers de fuego lento",
		Icon:  "🥶",
		Query: "Recommend slow-burn psychological thrillers that build tension gradually rather than rely on action set pieces.",
	},
	{
		Label: "Joyas en otros idiomas",
		Icon:  "🌍",
		Query: "Recommend acclaimed non-English films from around the world — Korean, Iranian, French, Japanese, Spanish, etc. Avoid the obvious mainstream picks.",
	},
	{
		Label: "Clásicos antes de 1990",
		Icon:  "📽️",
		Query: "Recommend essential film classics released before 1990 that hold up brilliantly today and would surprise a modern viewer.",
	},
	{
		Label: "Pelis para arruinar una primera cita",
		Icon:  "💀",
		Query: "Recommend deeply weird, uncomfortable, or unhinged films that would absolutely ruin a first date — the kind that leave you staring at the wall afterwards.",
	},
}

var defaultChipDefsDE = []defaultChipDef{
	{
		Label: "Preisträger des letzten Jahrzehnts",
		Icon:  "🏆",
		Query: "Recommend critically acclaimed films from the last decade that won major awards (Oscars, Cannes, BAFTA, Golden Globes). Mix genres.",
	},
	{
		Label: "Sci-Fi für den Kopf",
		Icon:  "🌌",
		Query: "Recommend mind-bending science fiction films with non-linear time, philosophical themes, or reality-warping concepts.",
	},
	{
		Label: "Langsam brennende Thriller",
		Icon:  "🥶",
		Query: "Recommend slow-burn psychological thrillers that build tension gradually rather than rely on action set pieces.",
	},
	{
		Label: "Perlen in anderen Sprachen",
		Icon:  "🌍",
		Query: "Recommend acclaimed non-English films from around the world — Korean, Iranian, French, Japanese, Spanish, etc. Avoid the obvious mainstream picks.",
	},
	{
		Label: "Klassiker vor 1990",
		Icon:  "📽️",
		Query: "Recommend essential film classics released before 1990 that hold up brilliantly today and would surprise a modern viewer.",
	},
	{
		Label: "Filme, die ein erstes Date ruinieren",
		Icon:  "💀",
		Query: "Recommend deeply weird, uncomfortable, or unhinged films that would absolutely ruin a first date — the kind that leave you staring at the wall afterwards.",
	},
}

var defaultChipDefsFR = []defaultChipDef{
	{
		Label: "Primés de la dernière décennie",
		Icon:  "🏆",
		Query: "Recommend critically acclaimed films from the last decade that won major awards (Oscars, Cannes, BAFTA, Golden Globes). Mix genres.",
	},
	{
		Label: "Science-fiction qui retourne le cerveau",
		Icon:  "🌌",
		Query: "Recommend mind-bending science fiction films with non-linear time, philosophical themes, or reality-warping concepts.",
	},
	{
		Label: "Thrillers à combustion lente",
		Icon:  "🥶",
		Query: "Recommend slow-burn psychological thrillers that build tension gradually rather than rely on action set pieces.",
	},
	{
		Label: "Pépites en langue étrangère",
		Icon:  "🌍",
		Query: "Recommend acclaimed non-English films from around the world — Korean, Iranian, French, Japanese, Spanish, etc. Avoid the obvious mainstream picks.",
	},
	{
		Label: "Classiques d'avant 1990",
		Icon:  "📽️",
		Query: "Recommend essential film classics released before 1990 that hold up brilliantly today and would surprise a modern viewer.",
	},
	{
		Label: "Films qui sabotent un premier rendez-vous",
		Icon:  "💀",
		Query: "Recommend deeply weird, uncomfortable, or unhinged films that would absolutely ruin a first date — the kind that leave you staring at the wall afterwards.",
	},
}

var defaultChipDefsPT = []defaultChipDef{
	{
		Label: "Premiados da última década",
		Icon:  "🏆",
		Query: "Recommend critically acclaimed films from the last decade that won major awards (Oscars, Cannes, BAFTA, Golden Globes). Mix genres.",
	},
	{
		Label: "Sci-fi que mexe com a cabeça",
		Icon:  "🌌",
		Query: "Recommend mind-bending science fiction films with non-linear time, philosophical themes, or reality-warping concepts.",
	},
	{
		Label: "Thrillers de queima lenta",
		Icon:  "🥶",
		Query: "Recommend slow-burn psychological thrillers that build tension gradually rather than rely on action set pieces.",
	},
	{
		Label: "Joias em outros idiomas",
		Icon:  "🌍",
		Query: "Recommend acclaimed non-English films from around the world — Korean, Iranian, French, Japanese, Spanish, etc. Avoid the obvious mainstream picks.",
	},
	{
		Label: "Clássicos antes de 1990",
		Icon:  "📽️",
		Query: "Recommend essential film classics released before 1990 that hold up brilliantly today and would surprise a modern viewer.",
	},
	{
		Label: "Filmes que detonam o primeiro encontro",
		Icon:  "💀",
		Query: "Recommend deeply weird, uncomfortable, or unhinged films that would absolutely ruin a first date — the kind that leave you staring at the wall afterwards.",
	},
}

var defaultChipDefsIT = []defaultChipDef{
	{
		Label: "Premiati dell'ultimo decennio",
		Icon:  "🏆",
		Query: "Recommend critically acclaimed films from the last decade that won major awards (Oscars, Cannes, BAFTA, Golden Globes). Mix genres.",
	},
	{
		Label: "Sci-fi che ti spacca la testa",
		Icon:  "🌌",
		Query: "Recommend mind-bending science fiction films with non-linear time, philosophical themes, or reality-warping concepts.",
	},
	{
		Label: "Thriller a fuoco lento",
		Icon:  "🥶",
		Query: "Recommend slow-burn psychological thrillers that build tension gradually rather than rely on action set pieces.",
	},
	{
		Label: "Gemme in altre lingue",
		Icon:  "🌍",
		Query: "Recommend acclaimed non-English films from around the world — Korean, Iranian, French, Japanese, Spanish, etc. Avoid the obvious mainstream picks.",
	},
	{
		Label: "Classici prima del 1990",
		Icon:  "📽️",
		Query: "Recommend essential film classics released before 1990 that hold up brilliantly today and would surprise a modern viewer.",
	},
	{
		Label: "Film che rovinano un primo appuntamento",
		Icon:  "💀",
		Query: "Recommend deeply weird, uncomfortable, or unhinged films that would absolutely ruin a first date — the kind that leave you staring at the wall afterwards.",
	},
}

// defaultChipDefsByLocale maps a normalized locale code to its chip set.
// Locales not present here fall back to English in defaultChips().
var defaultChipDefsByLocale = map[string][]defaultChipDef{
	"en": defaultChipDefsEN,
	"ru": defaultChipDefsRU,
	"es": defaultChipDefsES,
	"de": defaultChipDefsDE,
	"fr": defaultChipDefsFR,
	"pt": defaultChipDefsPT,
	"it": defaultChipDefsIT,
}

// defaultChips returns the static chip set for the given locale, with stable
// chip IDs derived from the labels.
func defaultChips(locale string) []Chip {
	defs, ok := defaultChipDefsByLocale[locale]
	if !ok {
		defs = defaultChipDefsEN
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
