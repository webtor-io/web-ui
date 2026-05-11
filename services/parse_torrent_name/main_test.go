package parsetorrentname

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"testing"
)

var updateGoldenFiles = flag.Bool("update", false, "update golden files in testdata/")

var testData = []string{
	"The Walking Dead S05E03 720p HDTV x264-ASAP[ettv]",
	"Hercules (2014) 1080p BrRip H264 - YIFY",
	"Dawn.of.the.Planet.of.the.Apes.2014.HDRip.XViD-EVO",
	"The Big Bang Theory S08E06 HDTV XviD-LOL [eztv]",
	"22 Jump Street (2014) 720p BrRip x264 - YIFY",
	"Hercules.2014.EXTENDED.1080p.WEB-DL.DD5.1.H264-RARBG",
	"Hercules.2014.Extended.Cut.HDRip.XViD-juggs[ETRG]",
	"Hercules (2014) WEBDL DVDRip XviD-MAX",
	"WWE Hell in a Cell 2014 PPV WEB-DL x264-WD -={SPARROW}=-",
	"UFC.179.PPV.HDTV.x264-Ebi[rartv]",
	"Marvels Agents of S H I E L D S02E05 HDTV x264-KILLERS [eztv]",
	"X-Men.Days.of.Future.Past.2014.1080p.WEB-DL.DD5.1.H264-RARBG",
	"Guardians Of The Galaxy 2014 R6 720p HDCAM x264-JYK",
	"Marvel's.Agents.of.S.H.I.E.L.D.S02E01.Shadows.1080p.WEB-DL.DD5.1",
	"Marvels Agents of S.H.I.E.L.D. S02E06 HDTV x264-KILLERS[ettv]",
	"Guardians of the Galaxy (CamRip / 2014)",
	"The.Walking.Dead.S05E03.1080p.WEB-DL.DD5.1.H.264-Cyphanix[rartv]",
	"Brave.2012.R5.DVDRip.XViD.LiNE-UNiQUE",
	"Lets.Be.Cops.2014.BRRip.XViD-juggs[ETRG]",
	"These.Final.Hours.2013.WBBRip XViD",
	"Downton Abbey 5x06 HDTV x264-FoV [eztv]",
	"Annabelle.2014.HC.HDRip.XViD.AC3-juggs[ETRG]",
	"Lucy.2014.HC.HDRip.XViD-juggs[ETRG]",
	"The Flash 2014 S01E04 HDTV x264-FUM[ettv]",
	"South Park S18E05 HDTV x264-KILLERS [eztv]",
	"The Flash 2014 S01E03 HDTV x264-LOL[ettv]",
	"The Flash 2014 S01E01 HDTV x264-LOL[ettv]",
	"Lucy 2014 Dual-Audio WEBRip 1400Mb",
	"Teenage Mutant Ninja Turtles (HdRip / 2014)",
	"Teenage Mutant Ninja Turtles (unknown_release_type / 2014)",
	"The Simpsons S26E05 HDTV x264 PROPER-LOL [eztv]",
	"2047 - Sights of Death (2014) 720p BrRip x264 - YIFY",
	"Two and a Half Men S12E01 HDTV x264 REPACK-LOL [eztv]",
	"Dinosaur 13 2014 WEBrip XviD AC3 MiLLENiUM",
	"Teenage.Mutant.Ninja.Turtles.2014.HDRip.XviD.MP3-RARBG",
	"Dawn.Of.The.Planet.of.The.Apes.2014.1080p.WEB-DL.DD51.H264-RARBG",
	"Teenage.Mutant.Ninja.Turtles.2014.720p.HDRip.x264.AC3.5.1-RARBG",
	"Gotham.S01E05.Viper.WEB-DL.x264.AAC",
	"Into.The.Storm.2014.1080p.WEB-DL.AAC2.0.H264-RARBG",
	"Lucy 2014 Dual-Audio 720p WEBRip",
	"Into The Storm 2014 1080p BRRip x264 DTS-JYK",
	"Sin.City.A.Dame.to.Kill.For.2014.1080p.BluRay.x264-SPARKS",
	"WWE Monday Night Raw 3rd Nov 2014 HDTV x264-Sir Paul",
	"Jack.And.The.Cuckoo-Clock.Heart.2013.BRRip XViD",
	"WWE Hell in a Cell 2014 HDTV x264 SNHD",
	"Dracula.Untold.2014.TS.XViD.AC3.MrSeeN-SiMPLE",
	"The Missing 1x01 Pilot HDTV x264-FoV [eztv]",
	"Doctor.Who.2005.8x11.Dark.Water.720p.HDTV.x264-FoV[rartv]",
	"Gotham.S01E07.Penguins.Umbrella.WEB-DL.x264.AAC",
	"One Shot [2014] DVDRip XViD-ViCKY",
	"The Shaukeens 2014 Hindi (1CD) DvDScr x264 AAC...Hon3y",
	"The Shaukeens (2014) 1CD DvDScr Rip x264 [DDR]",
	"Annabelle.2014.1080p.PROPER.HC.WEBRip.x264.AAC.2.0-RARBG",
	"Interstellar (2014) CAM ENG x264 AAC-CPG",
	"Guardians of the Galaxy (2014) Dual Audio DVDRip AVI",
	"Eliza Graves (2014) Dual Audio WEB-DL 720p MKV x264",
	"WWE Monday Night Raw 2014 11 10 WS PDTV x264-RKOFAN1990 -={SPARR",
	"Sons.of.Anarchy.S01E03",
	"doctor_who_2005.8x12.death_in_heaven.720p_hdtv_x264-fov",
	"breaking.bad.s01e01.720p.bluray.x264-reward",
	"Game of Thrones - 4x03 - Breaker of Chains",
	"[720pMkv.Com]_sons.of.anarchy.s05e10.480p.BluRay.x264-GAnGSteR",
	"[ www.Speed.cd ] -Sons.of.Anarchy.S07E07.720p.HDTV.X264-DIMENSION",
	"Community.s02e20.rus.eng.720p.Kybik.v.Kybe",
	"The.Jungle.Book.2016.3D.1080p.BRRip.SBS.x264.AAC-ETRG",
	"Ant-Man.2015.3D.1080p.BRRip.Half-SBS.x264.AAC-m2g",
	"Ice.Age.Collision.Course.2016.READNFO.720p.HDRIP.X264.AC3.TiTAN",
	"Red.Sonja.Queen.Of.Plagues.2016.BDRip.x264-W4F[PRiME]",
	"The Purge: Election Year (2016) HC - 720p HDRiP - 900MB - ShAaNi",
	"War Dogs (2016) HDTS 600MB - NBY",
	"The Hateful Eight (2015) 720p BluRay - x265 HEVC - 999MB - ShAaN",
	"The.Boss.2016.UNRATED.720p.BRRip.x264.AAC-ETRG",
	"Return.To.Snowy.River.1988.iNTERNAL.DVDRip.x264-W4F[PRiME]",
	"Akira (2016) - UpScaled - 720p - DesiSCR-Rip - Hindi - x264 - AC3 - 5.1 - Mafiaking - M2Tv",
	"Ben Hur 2016 TELESYNC x264 AC3 MAXPRO",
	"The.Secret.Life.of.Pets.2016.HDRiP.AAC-LC.x264-LEGi0N",
	"[HorribleSubs] Clockwork Planet - 10 [480p].mkv",
	"[HorribleSubs] Detective Conan - 862 [1080p].mkv",
	"thomas.and.friends.s19e09_s20e14.convert.hdtv.x264-w4f[eztv].mkv",
	"Blade.Runner.2049.2017.1080p.WEB-DL.DD5.1.H264-FGT-[rarbg.to]",
	"2012(2009).1080p.Dual Audio(Hindi+English) 5.1 Audios",
	"2012 (2009) 1080p BrRip x264 - 1.7GB - YIFY",
	"2012 2009 x264 720p Esub BluRay 6.0 Dual Audio English Hindi GOPISAHI",
	"Little.Girls.Love.Big.Ducks.11.[Crave.Media.2024].XXX.WEB-DL.540p.SPLIT.SCENES.[XC].Scene01",
	"www.1TamilMV.tf - Deadpool & Wolverine (2024) English TRUE WEB-DL - 4K SDR - HDR10+ - (DD+5.1 ATMOS - 768Kbps & AAC).mkv",
	"Delicious.2025.MVO.WEB-DLRip.NF.x264.p3rr3nt.mkv",
	"Bolshaya.dvadcatka.2025.AMZN.WEB-DLRip.AVC.mkv",
	"How to Lose a Guy in 10 Days [Как отделаться от парня за 10 дней] (2003) BDRip RusEngSubsChpt.mkv",
	"Этернавт - The Eternaut S01 E01 (Ночь игры в труко) WEB-DL 1080p (2025).mkv",
	"www.1TamilBlasters.moi - Havoc (2025) [1080p HD AVC - x264 - [Tam + Tel + Hin + Eng] - DDP 5.1 (768Kbps) ATMOS - 8.2GB - ESub].mkv",
	"[designcode.io]",
	// Year-range pattern: long-running series tagged "S01-S12.2007-2019".
	// Year matcher must take the FIRST year of the range (premiere year)
	// rather than the last. Regression test for the BBT-trilogy bug.
	"The.Big.Bang.Theory.S01-S12.2007-2019.BDRip-AVC",
	// Movie filename with size/release-year/codec tags after dashes —
	// "(1990 - 1997)" and "- 1080p" must NOT leak into Episode. Year
	// matcher runs first and consumes the range; episode regex's 1-3
	// digit cap blocks 1080.
	"www.1TamilMV.kiwi - Home Alone Trilogy (1990 - 1997) BluRay - 1080p - x264 - [Tam + Hin + Eng] - AAC - 5.5GB - ESub",
	// Trailing "- NNNN" tag on a movie filename used to flip the torrent
	// into series (episode=1046). Cap-to-3-digits keeps it as a plain
	// movie release.
	"Interestelar (2014) IMAX 1080p 6ch BrRip Dual Audio - 1046",
	// Multi-movie compilation with single-digit "part" markers. Episode
	// extraction is correct on this name in isolation; the classifier
	// fix elsewhere ensures sameTitle=false packs don't get treated as
	// SeriesSingleSeason.
	"Le Hobbit - 1 - Un Voyage Inattendu - 1080p.mkv",

	// ----- Adult content detection (Porn=true). Names below are
	// representative samples from production ai_enrich.query negative
	// cache. The downstream AI enrichment fallback should skip these.
	// Studio names: Blacked, Brazzers, Mylf/Milfy, OnlyFans, Manyvids,
	// Hegre, Wowgirls, Spankmonster, Momswapped, Maturenl, Mofos,
	// Voyeur-russian, Stickam, Latinpapixxl, hgshequ (Chinese BBS),
	// hhd800 (Chinese pirate site).
	"Blacked.18.03.21.Lana.Rhoades.1080p.mp4",
	"Brazzers - Wife and Stepdaughter Want Delivery Guy's Package",
	"Milfy 24 01 24 Richelle Ryan Curvy Fit Mom",
	"OnlyFans - Jolla and Keiran Lee",
	"manyvids - latina gets pounded and turns into a super squirter",
	"hegre 23 08 22 allie asia juicy orgasms",
	"wowgirls 20 07 08 hayli sanders and anna di fuck us",
	"spankmonster - milf explosion",
	"momswapped - tending to our stepmoms garden",
	"voyeur-russian nudism 130930",
	"stickam brimarr18 nude",
	"latinpapixxl latinpapixxl nude leaked onlyfans video coomeri50",
	"hgshequ cc@（@pdd68868）yinxiangzupaisheying",
	"hhd800 com@113024-001-carib",
	// Explicit English keywords without a studio name. Should still flag.
	"art of zoo - vixen ladys dog story creampie",
	"My Stepbrother And I Threesome Compilation",
	"Random.Title.Gangbang.Bukkake.2024.mp4",
	// "OnlyFans" abbreviated as "of -" at the start of the title.
	"of – jordan starr feeds john bronco his thick juicy cock",
	// BBC = "Big Black Cock" — must fire only when paired with an
	// adult anchor (cock/fuck/addict/etc.). "BBC News" must NOT flag.
	"tonyropeuk bbc addict british kerry louise",
	"Wifey - Kitana Collins - Hotwife Worships BBC for Husband",
	// JAV studio prefix + numeric code, both with and without separator.
	"IPVR-00265-1",
	"ABP-123 Uncensored",
	"[FC2PPV-1311003] Uncensored Leak",
	// Russian explicit. Cyrillic prefix guard must prevent "страх",
	// "требует", etc. from matching.
	"Трахаю мою сводную сестру пока мои родители в следующей комнате",
	"студентку жестко трахают и заполняют ее киску спермой с кримпаем",
	"06  партизаны 2026", // RU but NOT adult — guard regex
	// Chinese adult markers: 无码 (uncensored), 流出 (leaked), 内射, 探花.
	"乃々果花-无码流出fc2ppv-1202781",
	"极品女友【依云】冲刺内射极品名器馒头美穴",
	// ----- Negative cases — these must NOT be flagged as porn:
	// "Sex and the City" — plain "sex" doesn't trigger.
	"Sex and the City S01E01 720p HDTV x264",
	// "BBC News" — BBC without an adult anchor must not flag.
	"BBC News Special 2024",
	// "Naughty Dog" — game studio with "naughty" prefix.
	"Naughty Dog Studios Documentary 2023",
	// "Analyze This" — "anal" must not false-match "analyze".
	"Analyze This 1999 720p BrRip x264",
	// "Stand Up S13" — Russian-popular comedy show, parser sometimes
	// produces "06 партизаны" parsed_title; the show itself is clean.
	"Stand Up S13E06 WEB-DL 1080p",
	// Cyrillic word containing "трах" as substring inside a longer word
	// (страхование, страх) — must NOT match the adult regex.
	"Страхование жизни 2020 документальный фильм",

	// ----- Second-pass adult patterns: studios that slipped past the
	// first pass + bestiality + bate-date cam convention.
	"julesjordan 18 02 04 whitney wright",
	"nubilesporn - less hiding and more riding for my swap family",
	"exploitedcollegegirls addyson 19",
	"milflicious - slutty housewife creampie",
	"3 girl and dog sex in brazil getting so wet",
	"art of zoo dog fuck and semen collect",
	"alinajellybeana bate 090607 stickam",
	"brookenashh-bate-091108",
	// Negative for bate: "Bates Motel" (real series) must NOT match
	// the bate pattern because it lacks the trailing timestamp.
	"Bates.Motel.S01E01.HDTV.x264-LOL",

	// ----- Standalone season tag without following [ex] marker.
	// Real production torrent: 114423c8a3 — pack of all Shingeki No
	// Kyojin seasons, each in its own subfolder. Without season
	// extraction the classifier sees sameTitle=false across folders
	// and falls into MovieMultiple, firing N Claude calls instead of
	// one SeriesMultipleSeasons resolution.
	"[Judas] Shingeki no Kyojin S3 - 01.mkv",
	"[DKB] Shingeki no Kyojin - The Final Season S4 Pt. 1 - 01.mkv",
	"Shingeki No Kyojin S2 - 01",
	// Standalone S\d in folder-name form (no episode marker on this
	// segment — the file part carries it).
	"Shingeki no Kyojin S3 Pt. 1",
	// Bare season tag on a single-file release.
	"Doctor.Who.S08.E01.Deep.Breath.WEB-DL.1080p.mkv",
}

func TestParser(t *testing.T) {
	for i, fname := range testData {
		//if i != 90 {
		//	continue
		//}
		t.Run(fmt.Sprintf("golden_file_%03d", i), func(t *testing.T) {
			tor, err := Parse(&TorrentInfo{}, fname)
			if err != nil {
				t.Fatalf("test %v: parser error:\n  %v", i, err)
			}

			goldenFilename := filepath.Join("testdata", fmt.Sprintf("golden_file_%03d.json", i))

			if *updateGoldenFiles {
				buf, err := json.MarshalIndent(tor, "", "  ")
				if err != nil {
					t.Fatalf("error marshaling result: %v", err)
				}

				if err = ioutil.WriteFile(goldenFilename, buf, 0644); err != nil {
					t.Fatalf("unable to update golden file: %v", err)
				}
			}

			buf, err := ioutil.ReadFile(goldenFilename)
			if err != nil {
				t.Fatalf("error loading golden file: %v", err)
			}

			var want TorrentInfo
			err = json.Unmarshal(buf, &want)
			if err != nil {
				t.Fatalf("error unmarshalling golden file %v: %v", goldenFilename, err)
			}

			if !reflect.DeepEqual(*tor, want) {
				t.Fatalf("test %v: wrong result for %q\nwant:\n  %+v\ngot:\n  %+v", i, fname, want, *tor)
			}
		})
	}
}
