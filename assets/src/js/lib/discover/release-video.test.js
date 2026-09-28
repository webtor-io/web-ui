import { test } from 'node:test';
import assert from 'node:assert/strict';
import { releaseVideo, releaseText } from './release-video.js';

// Names shaped like the ones addons send: the torrent's name, sometimes an
// addon's own "4k DV | HDR10+" line, sometimes a pack's file under it.
const cases = [
    // [what it is, text, codec, hdr, dv5]
    ['x265 with a dot-separated name', 'Oppenheimer.2023.1080p.BluRay.x265.10bit.AAC.5.1-GRP', 'hevc', null, false],
    ['H.265 with a dot inside the word', 'Dune.Part.Two.2024.1080p.WEB-DL.DDP5.1.Atmos.H.265-GRP', 'hevc', null, false],
    ['h265 lower case', 'Show.S02E03.1080p.WEB.h265-GRP', 'hevc', null, false],
    ['HEVC in brackets', '[Group] Some Anime - 12 (1080p) [HEVC] [Multi Subs]', 'hevc', null, false],
    ['HEVC_x265 with underscores', '[Fansub]_Show_1178_[FHD_1080p][HEVC_x265][10Bit].mkv', 'hevc', null, false],
    ['x264', 'Movie.2019.1080p.BluRay.x264-GRP', 'avc', null, false],
    ['H264 with spaces', 'Movie 2019 1080p WEB-DL H264 AAC', 'avc', null, false],
    ['AVC remux', 'Movie.2008.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-GRP', 'avc', null, false],
    ['AV1', 'Movie.2023.2160p.WEB-DL.AV1.Opus.5.1-GRP', 'av1', null, false],
    ['no codec named', 'Movie (2021) [1080p] [WEBRip] [5.1] [GRP]', 'unknown', null, false],

    // "x265" and "HEVC" inside other words say nothing.
    ['x265 inside a word', 'Relax265.Sessions.2022.1080p.WEB.H264-GRP', 'avc', null, false],
    ['x.265 inside a word', 'Firefox.265.Edition.1080p.WEB.H264-GRP', 'avc', null, false],
    ['HEVC in the group name', 'Movie.2019.1080p.BluRay.x264-HEVCKiNGS', 'avc', null, false],
    ['HEVC10 glued to a digit', 'Movie.2019.1080p.HEVC10.WEB-GRP', 'unknown', null, false],
    // Two codecs named: a pack folder over its file. Not ours to guess.
    ['a pack saying x265 over a file saying x264', 'Show - Season 1 - x265\nShow.2016.S01E02.HDTV.x264-GRP.mp4', 'unknown', null, false],
    ['a group named HEVC after x264', 'Movie.2019.1080p.WEB-DL.H264-HEVC', 'unknown', null, false],

    // HDR words.
    ['HDR10', 'The.Brutalist.2024.2160p.UHD.BluRay.REMUX.HDR10.HEVC.TrueHD.Atmos-GRP', 'hevc', 'pq', false],
    ['HDR10+', 'Movie.2024.2160p.WEB-DL.DDP5.1.HDR10+.H.265-GRP', 'hevc', 'pq', false],
    ['HDR10Plus', 'Movie.2024.2160p.WEB-DL.HDR10Plus.x265-GRP', 'hevc', 'pq', false],
    ['bare HDR', 'Movie (2021) [2160p] [4K] [WEB] [HDR] [5.1]', 'unknown', 'pq', false],
    ['HDRip is not HDR', 'Movie.2019.HDRip.XviD.AC3-GRP', 'unknown', null, false],
    ['HDTV is not HDR', 'Show.S01E01.720p.HDTV.x264-GRP', 'avc', null, false],
    ['HLG before HDR', 'Documentary.2022.2160p.HLG.HDR.SDR.x265-GRP', 'hevc', 'hlg', false],

    // Dolby Vision.
    ['DV with HDR10: the HDR10 layer', 'Show.S01E01.2160p.WEB-DL.DDP5.1.DV.HDR10.H.265-GRP', 'hevc', 'pq', false],
    ['DoVi with HDR10', 'Movie.2023.2160p.BluRay.REMUX.DoVi.HDR10.HEVC.TrueHD-GRP', 'hevc', 'pq', false],
    ['DV only, WEB: profile 5 by name', 'Movie.2023.2160p.WEB-DL.DDP5.1.Atmos.DV.H.265-GRP', 'hevc', 'dv', true],
    ['Dolby Vision spelled out, WEB', 'Movie 2021 2160p WEB-DL Dolby Vision H265', 'hevc', 'dv', true],
    ['DV only, Blu-ray remux: profile 7, not 5', 'Movie.2020.2160p.BluRay.REMUX.DV.TrueHD.HEVC-GRP', 'hevc', 'dv', false],
    ['DV only, WEB hybrid: profile 8', 'Movie.2022.2160p.WEB-DL.DV.HYBRID.DDP5.1.H.265-GRP', 'hevc', 'dv', false],
    // DDP5.1 is audio, not "P5".
    ['DDP5.1 is not profile 5', 'Movie.2019.2160p.BluRay.DDP5.1.DV.HYBRID.x265-GRP', 'hevc', 'dv', false],
    ['profile 5 said outright, even next to HDR', 'Movie.2022.2160p.WEB-DL.DV.P5.HDR.x265-GRP', 'hevc', 'pq', true],
    ['profile 8 said outright', 'Movie.2022.2160p.WEB-DL.DoVi.P8.x265-GRP', 'hevc', 'dv', false],
    ['DVDRip is not Dolby Vision', 'Movie.1999.DVDRip.x264-GRP', 'avc', null, false],
    // An addon's own line, as Torrentio writes it, joined with the title.
    ['the addon line says DV | HDR10+', 'Addon\n4k DV | HDR10+\nMovie.2023.2160p.WEB-DL.DDP5.1.H265-GRP\n👤 42 💾 15.2 GB', 'hevc', 'pq', false],
];

for (const [what, text, codec, hdr, dv5] of cases) {
    test(`releaseVideo: ${what}`, () => {
        assert.deepEqual(releaseVideo(text), { codec, hdr, dv5 }, text);
    });
}

test('releaseVideo: nothing to read is unknown, not an exception', () => {
    for (const v of [undefined, null, '', 42]) {
        assert.deepEqual(releaseVideo(v), { codec: 'unknown', hdr: null, dv5: false });
    }
});

test('releaseText: the name, the title and the hinted file name, strings only', () => {
    assert.equal(releaseText({
        name: 'Addon\n1080p',
        title: 'Movie 2020 1080p',
        behaviorHints: { filename: 'Movie.2020.1080p.WEB.x265-GRP.mkv' },
    }), 'Addon\n1080p\nMovie 2020 1080p\nMovie.2020.1080p.WEB.x265-GRP.mkv');
    assert.equal(releaseText({ name: 'A', title: null, behaviorHints: 'x' }), 'A');
    assert.equal(releaseText(null), '');
    // The file name hint alone can name the codec.
    assert.equal(releaseVideo(releaseText({
        name: 'Addon\n1080p', title: 'Movie 2020 1080p',
        behaviorHints: { filename: 'Movie.2020.1080p.WEB.x265-GRP.mkv' },
    })).codec, 'hevc');
});
