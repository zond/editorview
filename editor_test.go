package editorview

import (
	"reflect"
	"regexp"
	"testing"
)

func makeToken(pos point, buffer []rune) *token {
	return &token{pos: pos, buffer: buffer}
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		text   string
		tokens []*token
	}{
		{
			text: `abc
<select-from>def<select-to>
ghi`,
			tokens: []*token{
				makeToken(point{0, 0}, nil).setStart(),
				makeToken(point{0, 0}, []rune{'a'}).setRune('a'),
				makeToken(point{1, 0}, []rune{'b'}).setRune('b'),
				makeToken(point{2, 0}, []rune{'c'}).setRune('c'),
				makeToken(point{3, 0}, nil).setNewLine(),
				makeToken(point{0, 1}, []rune("<select-from>")).setSelectStart(),
				makeToken(point{13, 1}, []rune{'d'}).setRune('d'),
				makeToken(point{14, 1}, []rune{'e'}).setRune('e'),
				makeToken(point{15, 1}, []rune{'f'}).setRune('f'),
				makeToken(point{16, 1}, []rune("<select-to>")).setSelectEnd(),
				makeToken(point{27, 1}, nil).setNewLine(),
				makeToken(point{0, 2}, []rune{'g'}).setRune('g'),
				makeToken(point{1, 2}, []rune{'h'}).setRune('h'),
				makeToken(point{2, 2}, []rune{'i'}).setRune('i'),
				makeToken(point{3, 2}, nil).setEof(),
			},
		},
	} {
		count := 0
		parseTokens(stringToRunes(tc.text), func(tok *token) {
			if len(tc.tokens) == 0 {
				t.Fatalf("Got more than %v tokens", count)
			}
			if isEq, err := tc.tokens[0].eq(tok); !isEq {
				t.Fatalf("Token %v wrong: %v", count, err)
			}
			tc.tokens = tc.tokens[1:]
			count++
		})
		if len(tc.tokens) > 0 {
			t.Fatalf("Wanted more tokens, got %v", count)
		}
	}
}

func TestFlattenWithIndex(t *testing.T) {
	for _, tc := range []struct {
		text        string
		flatRaw     string
		flatScreen  string
		rawIndex    []flatIndex
		screenIndex []flatIndex
	}{
		{
			text:       "ab<select-from>c<select-to>",
			flatRaw:    "ab<select-from>c<select-to>",
			flatScreen: "abc",
			rawIndex: []flatIndex{
				{
					raw:     point{0, 0},
					screen:  point{0, 0},
					flatRaw: 0,
				},
				{
					raw:     point{1, 0},
					screen:  point{1, 0},
					flatRaw: 1,
				},
				{
					raw:     point{2, 0},
					screen:  point{2, 0},
					flatRaw: 2,
				},
				{
					raw:     point{3, 0},
					screen:  point{2, 0},
					flatRaw: 3,
				},
				{
					raw:     point{4, 0},
					screen:  point{2, 0},
					flatRaw: 4,
				},
				{
					raw:     point{5, 0},
					screen:  point{2, 0},
					flatRaw: 5,
				},
				{
					raw:     point{6, 0},
					screen:  point{2, 0},
					flatRaw: 6,
				},
				{
					raw:     point{7, 0},
					screen:  point{2, 0},
					flatRaw: 7,
				},
				{
					raw:     point{8, 0},
					screen:  point{2, 0},
					flatRaw: 8,
				},
				{
					raw:     point{9, 0},
					screen:  point{2, 0},
					flatRaw: 9,
				},
				{
					raw:     point{10, 0},
					screen:  point{2, 0},
					flatRaw: 10,
				},
				{
					raw:     point{11, 0},
					screen:  point{2, 0},
					flatRaw: 11,
				},
				{
					raw:     point{12, 0},
					screen:  point{2, 0},
					flatRaw: 12,
				},
				{
					raw:     point{13, 0},
					screen:  point{2, 0},
					flatRaw: 13,
				},
				{
					raw:     point{14, 0},
					screen:  point{2, 0},
					flatRaw: 14,
				},
				{
					raw:     point{15, 0},
					screen:  point{2, 0},
					flatRaw: 15,
				},
				{
					raw:     point{16, 0},
					screen:  point{3, 0},
					flatRaw: 16,
				},
				{
					raw:     point{17, 0},
					screen:  point{3, 0},
					flatRaw: 17,
				},
				{
					raw:     point{18, 0},
					screen:  point{3, 0},
					flatRaw: 18,
				},
				{
					raw:     point{19, 0},
					screen:  point{3, 0},
					flatRaw: 19,
				},
				{
					raw:     point{20, 0},
					screen:  point{3, 0},
					flatRaw: 20,
				},
				{
					raw:     point{21, 0},
					screen:  point{3, 0},
					flatRaw: 21,
				},
				{
					raw:     point{22, 0},
					screen:  point{3, 0},
					flatRaw: 22,
				},
				{
					raw:     point{23, 0},
					screen:  point{3, 0},
					flatRaw: 23,
				},
				{
					raw:     point{24, 0},
					screen:  point{3, 0},
					flatRaw: 24,
				},
				{
					raw:     point{25, 0},
					screen:  point{3, 0},
					flatRaw: 25,
				},
				{
					raw:     point{26, 0},
					screen:  point{3, 0},
					flatRaw: 26,
				},
				{
					raw:     point{27, 0},
					screen:  point{3, 0},
					flatRaw: 27,
				},
			},
			screenIndex: []flatIndex{
				{
					raw:     point{0, 0},
					screen:  point{0, 0},
					flatRaw: 0,
				},
				{
					raw:     point{1, 0},
					screen:  point{1, 0},
					flatRaw: 1,
				},
				{
					raw:     point{15, 0},
					screen:  point{2, 0},
					flatRaw: 15,
				},
				{
					raw:     point{27, 0},
					screen:  point{3, 0},
					flatRaw: 27,
				},
			},
		},
		{
			text:       "abc",
			flatRaw:    "abc",
			flatScreen: "abc",
			rawIndex: []flatIndex{
				{
					raw:     point{0, 0},
					screen:  point{0, 0},
					flatRaw: 0,
				},
				{
					raw:     point{1, 0},
					screen:  point{1, 0},
					flatRaw: 1,
				},
				{
					raw:     point{2, 0},
					screen:  point{2, 0},
					flatRaw: 2,
				},
				{
					raw:     point{3, 0},
					screen:  point{3, 0},
					flatRaw: 3,
				},
			},
			screenIndex: []flatIndex{
				{
					raw:     point{0, 0},
					screen:  point{0, 0},
					flatRaw: 0,
				},
				{
					raw:     point{1, 0},
					screen:  point{1, 0},
					flatRaw: 1,
				},
				{
					raw:     point{2, 0},
					screen:  point{2, 0},
					flatRaw: 2,
				},
				{
					raw:     point{3, 0},
					screen:  point{3, 0},
					flatRaw: 3,
				},
			},
		},
		{
			text:       "a<color:ffffff:000000>bc",
			flatRaw:    "a<color:ffffff:000000>bc",
			flatScreen: "abc",
			rawIndex: []flatIndex{
				{
					raw:     point{0, 0},
					screen:  point{0, 0},
					flatRaw: 0,
				},
				{
					raw:     point{1, 0},
					screen:  point{1, 0},
					flatRaw: 1,
				},
				{
					raw:     point{2, 0},
					screen:  point{1, 0},
					flatRaw: 2,
				},
				{
					raw:     point{3, 0},
					screen:  point{1, 0},
					flatRaw: 3,
				},
				{
					raw:     point{4, 0},
					screen:  point{1, 0},
					flatRaw: 4,
				},
				{
					raw:     point{5, 0},
					screen:  point{1, 0},
					flatRaw: 5,
				},
				{
					raw:     point{6, 0},
					screen:  point{1, 0},
					flatRaw: 6,
				},
				{
					raw:     point{7, 0},
					screen:  point{1, 0},
					flatRaw: 7,
				},
				{
					raw:     point{8, 0},
					screen:  point{1, 0},
					flatRaw: 8,
				},
				{
					raw:     point{9, 0},
					screen:  point{1, 0},
					flatRaw: 9,
				},
				{
					raw:     point{10, 0},
					screen:  point{1, 0},
					flatRaw: 10,
				},
				{
					raw:     point{11, 0},
					screen:  point{1, 0},
					flatRaw: 11,
				},
				{
					raw:     point{12, 0},
					screen:  point{1, 0},
					flatRaw: 12,
				},
				{
					raw:     point{13, 0},
					screen:  point{1, 0},
					flatRaw: 13,
				},
				{
					raw:     point{14, 0},
					screen:  point{1, 0},
					flatRaw: 14,
				},
				{
					raw:     point{15, 0},
					screen:  point{1, 0},
					flatRaw: 15,
				},
				{
					raw:     point{16, 0},
					screen:  point{1, 0},
					flatRaw: 16,
				},
				{
					raw:     point{17, 0},
					screen:  point{1, 0},
					flatRaw: 17,
				},
				{
					raw:     point{18, 0},
					screen:  point{1, 0},
					flatRaw: 18,
				},
				{
					raw:     point{19, 0},
					screen:  point{1, 0},
					flatRaw: 19,
				},
				{
					raw:     point{20, 0},
					screen:  point{1, 0},
					flatRaw: 20,
				},
				{
					raw:     point{21, 0},
					screen:  point{1, 0},
					flatRaw: 21,
				},
				{
					raw:     point{22, 0},
					screen:  point{1, 0},
					flatRaw: 22,
				},
				{
					raw:     point{23, 0},
					screen:  point{2, 0},
					flatRaw: 23,
				},
				{
					raw:     point{24, 0},
					screen:  point{3, 0},
					flatRaw: 24,
				},
			},
			screenIndex: []flatIndex{
				{
					raw:     point{0, 0},
					screen:  point{0, 0},
					flatRaw: 0,
				},
				{
					raw:     point{22, 0},
					screen:  point{1, 0},
					flatRaw: 22,
				},
				{
					raw:     point{23, 0},
					screen:  point{2, 0},
					flatRaw: 23,
				},
				{
					raw:     point{24, 0},
					screen:  point{3, 0},
					flatRaw: 24,
				},
			},
		},
	} {
		gotRaw, gotScreen, gotRawIndex, gotScreenIndex := flattenWithIndex(stringToRunes(tc.text))
		if string(gotRaw) != string(tc.flatRaw) {
			t.Fatalf("Got raw %q, wanted %q", string(gotRaw), string(tc.flatRaw))
		}
		if string(gotScreen) != string(tc.flatScreen) {
			t.Fatalf("Got scren %q, wanted %q", string(gotScreen), string(tc.flatScreen))
		}
		if !reflect.DeepEqual(gotRawIndex, tc.rawIndex) {
			t.Fatalf("Got raw index\n%+v\n, wanted\n%+v", gotRawIndex, tc.rawIndex)
		}
		if !reflect.DeepEqual(gotScreenIndex, tc.screenIndex) {
			t.Fatalf("Got screen index\n%+v\n, wanted\n%+v", gotScreenIndex, tc.screenIndex)
		}
	}
}

func TestReplace(t *testing.T) {
	for _, tc := range []struct {
		text      string
		reg       *regexp.Regexp
		repl      string
		raw       bool
		match     string
		rawSeg    segment
		screenSeg segment
		result    string
	}{
		{
			text:      "abc",
			reg:       regexp.MustCompile("a"),
			repl:      "d",
			raw:       true,
			match:     "a",
			rawSeg:    segment{{0, 0}, {1, 0}},
			screenSeg: segment{{0, 0}, {1, 0}},
			result:    "dbc",
		},
		{
			text:      "ab<select-from>c<select-to>",
			reg:       selectionPattern,
			repl:      "",
			raw:       true,
			match:     "<select-from>c<select-to>",
			rawSeg:    segment{{2, 0}, {27, 0}},
			screenSeg: segment{{2, 0}, {3, 0}},
			result:    "ab",
		},
		{
			text:      "ab\nde<select-from>cfg\nhi\n<select-to>j",
			reg:       selectionPattern,
			repl:      "",
			raw:       true,
			match:     "<select-from>cfg\nhi\n<select-to>",
			rawSeg:    segment{{2, 1}, {11, 3}},
			screenSeg: segment{{2, 1}, {0, 3}},
			result:    "ab\ndej",
		},
	} {
		got := replace(stringToRunes(tc.text), tc.raw, tc.reg, tc.repl, func(match string, rawSeg, screenSeg segment) bool {
			if match != tc.match {
				t.Errorf("Got match %q, wanted %q", match, tc.match)
			}
			if rawSeg != tc.rawSeg {
				t.Errorf("Got raw segment %+v, wanted %+v", rawSeg, tc.rawSeg)
			}
			if screenSeg != tc.screenSeg {
				t.Errorf("Got screen segment %+v, wanted %+v", screenSeg, tc.screenSeg)
			}
			return true
		})
		if gots := runesToString(got); gots != tc.result {
			t.Errorf("Got result %q, wanted %q", gots, tc.result)
		}

	}
}
