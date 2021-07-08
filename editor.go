package editorview

import (
	"bytes"
	"fmt"
	"log"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/sergi/go-diff/diffmatchpatch"
)

var (
	whitespacePattern = regexp.MustCompile("\\s+")
	selectFromToken   = "<select-from>"
	selectFromPattern = regexp.MustCompile(selectFromToken)
	selectToToken     = "<select-to>"
	selectToPattern   = regexp.MustCompile(selectToToken)
	selectionPattern  = regexp.MustCompile(fmt.Sprintf("(?s)(%s|%s)(.*)(%s|%s)", selectToPattern, selectFromPattern, selectToPattern, selectFromPattern))
	colorTagPattern   = regexp.MustCompile("<color:([A-Fa-f0-9]{6,6}):([A-Fa-f0-9]{6,6})>")
)

func Escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "&", "&amp;"), "<", "&lt;"), ">", "&gt;")
}

type point struct {
	x int
	y int
}

func (p point) dist(o point) int {
	dx := float64(p.x - o.x)
	dy := float64(p.y - o.y)
	return int(math.Round(math.Sqrt(dx*dx + dy*dy)))
}

func (p point) clone() *point {
	cpy := p
	return &cpy
}

type segment [2]point

func (s segment) len() int {
	return s[0].dist(s[1])
}

type points []point

func (p points) Len() int {
	return len(p)
}

func (p points) Less(i, j int) bool {
	if p[i].y < p[j].y {
		return true
	} else if p[i].y == p[j].y {
		return p[i].x < p[j].x
	}
	return false
}

func (p points) Swap(i, j int) {
	p[j], p[i] = p[i], p[j]
}

type patch struct {
	cursor  point
	patches []diffmatchpatch.Patch
}

type Editor struct {
	Screen      tcell.Screen
	EventFilter func(tcell.Event) []tcell.Event

	// Edited text: [line][rune]
	rawBuffer [][]rune
	// Line wrapped edited text: [line][rune]
	screenBuffer [][]rune
	// Index from [line][rune] of wrapped edited text to point{x: rune, y: line} of raw buffer.
	// Every line has a single point{x: -1, y: lineIdx} appended at the end to mark both cursor
	// positions at the 'newline position' and lineIdx for empty lines.
	screenBufferIndex [][]point
	// number of screenBuffer lines hidden above screen
	lineOffset int

	selecting   bool
	pasteBuffer [][]rune
	undoPatches []patch
	redoPatches []patch
	cursor      point
	differ      *diffmatchpatch.DiffMatchPatch
}

func (e *Editor) runeAt(screenPoint point) rune {
	scrolledPoint := point{x: screenPoint.x, y: screenPoint.y + e.lineOffset}
	if scrolledPoint.y < len(e.screenBuffer) {
		if scrolledPoint.x < len(e.screenBuffer[scrolledPoint.y]) {
			return e.screenBuffer[scrolledPoint.y][scrolledPoint.x]
		}
		return '\n'
	}
	return 0
}

func (e *Editor) differentWhitespaceness(screenPoint point) func(screenPoint point) bool {
	currWhitespaceness := whitespacePattern.MatchString(string([]rune{e.runeAt(screenPoint)}))
	return func(screenPoint point) bool {
		return currWhitespaceness != whitespacePattern.MatchString(string([]rune{e.runeAt(screenPoint)}))
	}
}

func (e *Editor) indentation(screenY int) int {
	p := e.screenBufferIndex[screenY+e.lineOffset][0]
	for x, r := range e.rawBuffer[p.y] {
		if whitespacePattern.MatchString(string([]rune{r})) {
			return x
		}
	}
	return 0
}

func (e *Editor) differentIndentness(screenPoint point) func(screenPoint point) bool {
	currIndentness := e.indentation(screenPoint.y)
	return func(screenPoint point) bool {
		return currIndentness != e.indentation(screenPoint.y)
	}
}

func (e *Editor) moveCursorUntil(dir direction, cont func(screenPoint point) bool) {
	if cont != nil {
		for e.moveCursor(dir) {
			if cont(e.cursor) {
				break
			}
		}
	} else {
		e.moveCursor(dir)
	}
}

func (e *Editor) addLineAt(screenPoint point) {
	defer e.redraw()
	p := e.screenBufferIndex[screenPoint.y+e.lineOffset][screenPoint.x]
	if p.x < 0 {
		e.rawBuffer = concatRuneLines(
			e.rawBuffer[:p.y+1],
			[][]rune{nil},
			e.rawBuffer[p.y+1:],
		)
		return
	}
	e.rawBuffer = concatRuneLines(
		e.rawBuffer[:p.y],
		[][]rune{e.rawBuffer[p.y][:p.x]},
		[][]rune{e.rawBuffer[p.y][p.x:]},
		e.rawBuffer[p.y+1:],
	)
}

func (e *Editor) deleteAt(screenPoint point) {
	defer e.redraw()
	p := e.screenBufferIndex[screenPoint.y+e.lineOffset][screenPoint.x]
	if p.x < 0 {
		if p.y+1 < len(e.rawBuffer) {
			e.rawBuffer = concatRuneLines(
				e.rawBuffer[:p.y],
				[][]rune{concatRunes(e.rawBuffer[p.y], e.rawBuffer[p.y+1])},
				e.rawBuffer[p.y+2:],
			)
		}
		return
	}
	if e.rawBuffer[p.y][p.x] == '&' {
		if len(e.rawBuffer[p.y])-p.x > 5 && string(e.rawBuffer[p.y][p.x:p.x+5]) == "&amp;" {
			e.rawBuffer[p.y] = concatRunes(e.rawBuffer[p.y][:p.x], e.rawBuffer[p.y][p.x+5:])
			return
		} else if len(e.rawBuffer[p.y])-p.x > 4 && string(e.rawBuffer[p.y][p.x:p.x+4]) == "&gt;" {
			e.rawBuffer[p.y] = concatRunes(e.rawBuffer[p.y][:p.x], e.rawBuffer[p.y][p.x+4:])
			return
		} else if len(e.rawBuffer[p.y])-p.x > 4 && string(e.rawBuffer[p.y][p.x:p.x+4]) == "&lt;" {
			e.rawBuffer[p.y] = concatRunes(e.rawBuffer[p.y][:p.x], e.rawBuffer[p.y][p.x+4:])
			return
		}
	}
	e.rawBuffer[p.y] = concatRunes(e.rawBuffer[p.y][:p.x], e.rawBuffer[p.y][p.x+1:])
}

func PlainText(s string) string {
	return runesToString(plain(stringToRunes(s)))
}

func plain(r [][]rune) [][]rune {
	res := [][]rune{}
	parseTokens(r, func(t *token) {
		if t.start {
			res = append(res, nil)
		} else if t.rune != nil {
			res[len(res)-1] = append(res[len(res)-1], *t.rune)
		} else if t.newLine {
			res = append(res, nil)
		}
	})
	return res
}

type flatIndex struct {
	raw     point
	screen  point
	flatRaw int
}

func flattenWithIndex(rs [][]rune) (flatRaw, flatScreen []rune, rawIndex, screenIndex []flatIndex) {
	screenPos := point{}
	parseTokens(rs, func(t *token) {
		if t.rune != nil {
			rawPos := t.pos
			offset := 0
			for _ = range t.buffer {
				rawIndex = append(rawIndex, flatIndex{raw: rawPos, screen: screenPos, flatRaw: len(flatRaw) + offset})
				rawPos.x++
				offset++
			}

			screenIndex = append(screenIndex, flatIndex{raw: t.pos, screen: screenPos, flatRaw: len(flatRaw)})
			screenPos.x++

			flatRaw = concatRunes(flatRaw, t.buffer)
			flatScreen = append(flatScreen, *t.rune)
		} else if t.newLine {
			rawIndex = append(rawIndex, flatIndex{raw: t.pos, screen: screenPos, flatRaw: len(flatRaw)})

			screenIndex = append(screenIndex, flatIndex{raw: t.pos, screen: screenPos, flatRaw: len(flatRaw)})
			screenPos.y++
			screenPos.x = 0

			flatRaw = append(flatRaw, '\n')
			flatScreen = append(flatScreen, '\n')
		} else if t.eof {
			rawIndex = append(rawIndex, flatIndex{raw: t.pos, screen: screenPos, flatRaw: len(flatRaw)})
			screenIndex = append(screenIndex, flatIndex{raw: t.pos, screen: screenPos, flatRaw: len(flatRaw)})
		} else {
			pos := t.pos
			offset := 0
			for _ = range t.buffer {
				rawIndex = append(rawIndex, flatIndex{raw: pos, screen: screenPos, flatRaw: len(flatRaw) + offset})
				pos.x++
				offset++
			}

			flatRaw = concatRunes(flatRaw, t.buffer)
		}
	})
	return flatRaw, flatScreen, rawIndex, screenIndex
}

func (e *Editor) replace(raw bool, p *regexp.Regexp, repl string, query func(match string, rawSeg, screenSeg segment) bool) {
	changedAnything := false
	e.rawBuffer = replace(e.rawBuffer, raw, p, repl, func(match string, rawSeg, screenSeg segment) bool {
		res := query(match, rawSeg, screenSeg)
		if res {
			changedAnything = true
		}
		return res
	})
	if changedAnything {
		e.redraw()
	}
}

func replace(rs [][]rune, raw bool, p *regexp.Regexp, repl string, query func(match string, rawSeg, screenSeg segment) bool) [][]rune {
	flatRaw, flatScreen, rawIndex, screenIndex := flattenWithIndex(rs)

	resRunes := make([]rune, len(flatRaw))
	copy(resRunes, flatRaw)

	haystack := flatScreen
	haystackIndex := screenIndex
	if raw {
		haystack = flatRaw
		haystackIndex = rawIndex
	}

	haystackOffset := 0
	for {
		remainingHaystack := haystack[haystackOffset:]
		remainingHaystackIndex := haystackIndex[haystackOffset:]

		searchString := string(remainingHaystack)
		matchIndex := p.FindStringIndex(searchString)
		if matchIndex == nil {
			return stringToRunes(string(resRunes))
		}

		startIndex := remainingHaystackIndex[len([]rune(searchString[:matchIndex[0]]))]
		endIndex := remainingHaystackIndex[len([]rune(searchString[:matchIndex[1]]))]

		if query(searchString[matchIndex[0]:matchIndex[1]], segment{startIndex.raw, endIndex.raw}, segment{startIndex.screen, endIndex.screen}) {
			replacement := p.ReplaceAllString(searchString[matchIndex[0]:matchIndex[1]], repl)
			resRunes = concatRunes(resRunes[:startIndex.flatRaw], []rune(replacement), []rune(searchString[matchIndex[1]:]))
		}
		haystackOffset += len([]rune(searchString[:matchIndex[1]]))
		if haystackOffset > len(haystack)-1 {
			return stringToRunes(string(resRunes))
		}
	}
}

func concatRuneLines(rs ...[][]rune) [][]rune {
	res := [][]rune{}
	for _, r := range rs {
		res = append(res, r...)
	}
	return res
}

func concatRunes(rs ...[]rune) []rune {
	res := []rune{}
	for _, r := range rs {
		res = append(res, r...)
	}
	return res
}

func (e *Editor) writeAt(runes []rune, screenPoint point) {
	defer e.redraw()
	p := e.screenBufferIndex[screenPoint.y+e.lineOffset][screenPoint.x]
	if p.x < 0 {
		e.rawBuffer[p.y] = concatRunes(e.rawBuffer[p.y], runes)
		return
	}
	e.rawBuffer[p.y] = concatRunes(
		e.rawBuffer[p.y][:p.x],
		runes,
		e.rawBuffer[p.y][p.x:],
	)
}

func (e *Editor) debuglog() {
	for _, l := range e.rawBuffer {
		log.Printf("%q", string(l))
	}
}

func (e *Editor) copySelection() {
	e.replace(true, selectionPattern, "", func(s string, rawSeg, screenSeg segment) bool {
		if match := selectionPattern.FindStringSubmatch(s); match != nil {
			e.pasteBuffer = plain(stringToRunes(match[2]))
		}
		return false
	})
}

func runesToString(rs [][]rune) string {
	res := &bytes.Buffer{}
	for idx, line := range rs {
		fmt.Fprintf(res, "%v", string(line))
		if idx+1 < len(rs) {
			fmt.Fprintln(res)
		}
	}
	return res.String()
}

func (e *Editor) removeSelection(cpy bool) (removedScreenSeg segment, removedRunes []rune) {
	e.replace(true, selectionPattern, "", func(s string, rawSeg, screenSeg segment) bool {
		if cpy {
			if match := selectionPattern.FindStringSubmatch(s); match != nil {
				e.pasteBuffer = plain(stringToRunes(match[2]))
			}
		}
		removedScreenSeg = screenSeg
		removedRunes = []rune(runesToString(plain(stringToRunes(s))))
		return true
	})
	return
}

func (e *Editor) backCursor(removedSeg segment, removedRunes []rune) {
	ps := points{removedSeg[0], removedSeg[1], e.cursor}
	sort.Sort(ps)
	if ps[1] == e.cursor || (ps[2] == e.cursor && ps[1].dist(ps[2]) == 1) {
		e.cursor = removedSeg[0]
	} else if ps[2] == e.cursor && e.cursor.y == ps[1].y {
		for i := 0; i < len(removedRunes); i++ {
			e.cursor.x--
		}
	} else if ps[2] == e.cursor {
		lines := strings.Count(string(removedRunes), "\n")
		for i := 0; i < lines; i++ {
			e.cursor.y--
		}
	}
	e.setCursor()
}

func (e *Editor) pollKeys() {
	var selectFrom *point
	for {
		evs := []tcell.Event{e.Screen.PollEvent()}
		if e.EventFilter != nil {
			evs = e.EventFilter(evs[0])
		}
		for _, untypedEv := range evs {
			prevContent := runesToString(e.rawBuffer)
			prevCursor := e.cursor
			storeUndo := true
			clearRedo := true
			selectFrom = nil

			switch ev := untypedEv.(type) {
			case *tcell.EventResize:
				e.redraw()
				e.setCursor()
			case *tcell.EventKey:
				switch ev.Key() {
				case tcell.KeyEnter:
					e.addLineAt(e.cursor)
					e.moveCursor(right)
				case tcell.KeyBackspace:
					e.moveCursor(left)
					whitespaceness := whitespacePattern.MatchString(string([]rune{e.runeAt(e.cursor)}))
					e.deleteAt(e.cursor)
					for e.moveCursor(left) {
						if whitespaceness != whitespacePattern.MatchString(string([]rune{e.runeAt(e.cursor)})) {
							e.moveCursor(right)
							break
						}
						e.deleteAt(e.cursor)
					}
				case tcell.KeyBackspace2:
					removedSeg, removedRunes := e.removeSelection(false)
					if len(removedRunes) == 0 {
						if e.moveCursor(left) {
							e.deleteAt(e.cursor)
						}
					} else {
						e.backCursor(removedSeg, removedRunes)
					}
				case tcell.KeyDelete:
					removedSeg, removedRunes := e.removeSelection(false)
					if len(removedRunes) == 0 {
						e.deleteAt(e.cursor)
					} else {
						e.backCursor(removedSeg, removedRunes)
					}
				case tcell.KeyTab:
					e.writeAt([]rune{' '}, e.cursor)
					e.moveCursor(right)
					for e.cursor.x%4 != 0 {
						e.writeAt([]rune{' '}, e.cursor)
						e.moveCursor(right)
					}
				case tcell.KeyRune:
					e.writeAt([]rune(Escape(string([]rune{ev.Rune()}))), e.cursor)
					e.moveCursor(right)
				case tcell.KeyPgUp:
					if ev.Modifiers()&tcell.ModShift != 0 {
						selectFrom = e.cursor.clone()
					}
					_, height := e.Screen.Size()
					for i := 0; i < height; i++ {
						if !e.moveCursor(up) {
							break
						}
					}
				case tcell.KeyPgDn:
					if ev.Modifiers()&tcell.ModShift != 0 {
						selectFrom = e.cursor.clone()
					}
					_, height := e.Screen.Size()
					for i := 0; i < height; i++ {
						if !e.moveCursor(down) {
							break
						}
					}
				case tcell.KeyHome:
					if ev.Modifiers()&tcell.ModShift != 0 {
						selectFrom = e.cursor.clone()
					}
					e.cursor.x = 0
					e.cursor.y = 0
					e.lineOffset = 0
					e.redraw()
					e.setCursor()
				case tcell.KeyEnd:
					if ev.Modifiers()&tcell.ModShift != 0 {
						selectFrom = e.cursor.clone()
					}
					_, height := e.Screen.Size()
					e.lineOffset = e.maxInt(0, len(e.screenBuffer)-height/2)
					e.cursor.y = len(e.screenBuffer) - e.lineOffset - 1
					e.cursor.x = len(e.screenBuffer[e.cursor.y])
					e.redraw()
					e.setCursor()
				case tcell.KeyCtrlZ:
					storeUndo = false
					clearRedo = false
					if len(e.undoPatches) > 0 {
						toApply := e.undoPatches[len(e.undoPatches)-1]
						e.undoPatches = e.undoPatches[:len(e.undoPatches)-1]
						newContent, applied := e.differ.PatchApply(toApply.patches, prevContent)
						if applied[0] {
							e.redoPatches = append(e.redoPatches, patch{patches: e.differ.PatchMake(newContent, prevContent), cursor: toApply.cursor})

							e.rawBuffer = stringToRunes(newContent)
							e.cursor = toApply.cursor

							e.redraw()
						}
					}
				case tcell.KeyCtrlY:
					clearRedo = false
					if len(e.redoPatches) > 0 {
						toApply := e.redoPatches[len(e.redoPatches)-1]
						e.redoPatches = e.redoPatches[:len(e.redoPatches)-1]
						newContent, applied := e.differ.PatchApply(toApply.patches, prevContent)
						if applied[0] {
							e.rawBuffer = stringToRunes(newContent)
							e.cursor = toApply.cursor
							e.redraw()
						}
					}
				case tcell.KeyCtrlC:
					e.copySelection()
				case tcell.KeyCtrlX:
					e.removeSelection(true)
					e.setCursor()
				case tcell.KeyCtrlV:
					if len(e.pasteBuffer) > 0 {
						for idx, line := range e.pasteBuffer {
							e.writeAt([]rune(Escape(string(line))), e.cursor)
							for _ = range line {
								e.moveCursor(right)
							}
							if idx+1 < len(e.pasteBuffer) {
								e.addLineAt(e.cursor)
								e.moveCursor(right)
							}
						}
					}
				case tcell.KeyCtrlW:
					e.Screen.Fini()
					return
				case tcell.KeyUp:
					if ev.Modifiers()&tcell.ModShift != 0 {
						selectFrom = e.cursor.clone()
					}
					if ev.Modifiers()&tcell.ModCtrl != 0 {
						e.moveCursorUntil(up, e.differentIndentness(e.cursor))
					} else {
						e.moveCursor(up)
					}
				case tcell.KeyDown:
					if ev.Modifiers()&tcell.ModShift != 0 {
						selectFrom = e.cursor.clone()
					}
					if ev.Modifiers()&tcell.ModCtrl != 0 {
						e.moveCursorUntil(down, e.differentIndentness(e.cursor))
					} else {
						e.moveCursor(down)
					}
				case tcell.KeyLeft:
					if ev.Modifiers()&tcell.ModShift != 0 {
						selectFrom = e.cursor.clone()
					}
					if ev.Modifiers()&tcell.ModCtrl != 0 {
						e.moveCursorUntil(left, e.differentWhitespaceness(e.cursor))
					} else {
						e.moveCursor(left)
					}
				case tcell.KeyRight:
					if ev.Modifiers()&tcell.ModShift != 0 {
						selectFrom = e.cursor.clone()
					}
					if ev.Modifiers()&tcell.ModCtrl != 0 {
						e.moveCursorUntil(right, e.differentWhitespaceness(e.cursor))
					} else {
						e.moveCursor(right)
					}
				case tcell.KeyEsc:
					e.selecting = false
					selectFrom = nil
					e.replace(true, selectToPattern, "", func(string, segment, segment) bool {
						return true
					})
					e.replace(true, selectFromPattern, "", func(string, segment, segment) bool {
						return true
					})
				}
			}
			if e.selecting {
				if selectFrom == nil {
					e.selecting = false
				} else {
					e.replace(true, selectToPattern, "", func(string, segment, segment) bool {
						return true
					})
					e.writeAt([]rune(selectToToken), e.cursor)
				}
			} else {
				if selectFrom != nil {
					e.selecting = true
					e.replace(true, selectToPattern, "", func(string, segment, segment) bool {
						return true
					})
					e.replace(true, selectFromPattern, "", func(string, segment, segment) bool {
						return true
					})
					ps := points{*selectFrom, e.cursor}
					sort.Sort(ps)
					for _, idx := range []int{1, 0} {
						p := ps[idx]
						if p == e.cursor {
							e.writeAt([]rune(selectToToken), p)
						} else {
							e.writeAt([]rune(selectFromToken), p)
						}
					}
				}
			}
			if storeUndo {
				if newContent := runesToString(e.rawBuffer); newContent != prevContent {
					e.undoPatches = append(e.undoPatches, patch{patches: e.differ.PatchMake(newContent, prevContent), cursor: prevCursor})
				}
			}
			if clearRedo {
				e.redoPatches = nil
			}
			e.Screen.ShowCursor(e.cursor.x, e.cursor.y)
			e.Screen.Show()
		}
	}
}

type direction uint8

const (
	up direction = iota
	left
	down
	right
)

func (d direction) String() string {
	switch d {
	case up:
		return "up"
	case left:
		return "left"
	case down:
		return "down"
	case right:
		return "right"
	}
	return fmt.Sprintf("unknown:%v", int(d))
}

func (e *Editor) canScroll(d direction) bool {
	switch d {
	case up:
		return e.lineOffset > 0
	case down:
		_, height := e.Screen.Size()
		return e.lineOffset+1 < len(e.screenBuffer)-height/2
	}
	return false
}

func (e *Editor) scroll(d direction) {
	width, height := e.Screen.Size()
	if width == 0 || height == 0 {
		return
	}
	switch d {
	case up:
		e.lineOffset--
	case down:
		e.lineOffset++
	}
	e.limitInt(&e.lineOffset, 0, len(e.screenBuffer)-height/2)
	e.redraw()
}

func (e *Editor) moveCursor(d direction) bool {
	defer e.setCursor()
	switch d {
	case up:
		if e.canMoveCursor(up) {
			e.cursor.y--
			return true
		} else if e.canScroll(up) {
			e.scroll(up)
			return true
		}
	case left:
		if e.canMoveCursor(left) {
			e.cursor.x--
			return true
		} else if e.canMoveCursor(up) {
			e.cursor.y--
			e.cursor.x = len(e.screenBuffer[e.cursor.y+e.lineOffset])
			return true
		} else if e.canScroll(up) {
			e.scroll(up)
			e.cursor.x = len(e.screenBuffer[e.cursor.y+e.lineOffset])
			return e.moveCursor(left)
		}
	case down:
		if e.canMoveCursor(down) {
			e.cursor.y++
			return true
		} else if e.canScroll(down) {
			e.scroll(down)
			return true
		}
	case right:
		if e.canMoveCursor(right) {
			e.cursor.x++
			return true
		} else if e.canMoveCursor(down) {
			e.cursor.y++
			e.cursor.x = 0
			return true
		} else if e.cursor.y+e.lineOffset+1 < len(e.screenBuffer) && e.canScroll(down) {
			e.scroll(down)
			e.cursor.x = 0
			return true
		}
	}
	return false
}

func (e *Editor) limitInt(i *int, minInc, maxExc int) {
	if *i < minInc {
		*i = minInc
	}
	if *i >= maxExc {
		*i = maxExc - 1
	}
}

func (e *Editor) minInt(i ...int) int {
	res := int(math.MaxInt64)
	for _, j := range i {
		if j < res {
			res = j
		}
	}
	return res
}

func (e *Editor) maxInt(i ...int) int {
	res := int(math.MinInt64)
	for _, j := range i {
		if j > res {
			res = j
		}
	}
	return res
}

func (e *Editor) lineWidth(y int) int {
	if y+e.lineOffset < len(e.screenBuffer) {
		return len(e.screenBuffer[y+e.lineOffset])
	}
	return 0
}

func (e *Editor) setCursor() {
	width, height := e.Screen.Size()
	if width == 0 || height == 0 {
		return
	}
	e.limitInt(&e.cursor.y, 0, e.minInt(height, len(e.screenBuffer)-e.lineOffset))
	e.limitInt(&e.cursor.x, 0, e.minInt(width, e.lineWidth(e.cursor.y)+1))
}

func (e *Editor) canMoveCursor(d direction) bool {
	width, height := e.Screen.Size()
	switch d {
	case up:
		return e.cursor.y > 0
	case left:
		return e.cursor.x > 0
	case down:
		return e.cursor.y+1 < height && e.cursor.y+e.lineOffset < len(e.screenBuffer)-1
	case right:
		return e.cursor.x+1 < width && e.cursor.x < e.lineWidth(e.cursor.y)
	}
	return false
}

type token struct {
	buffer      []rune
	pos         point
	rune        *rune
	style       *tcell.Style
	newLine     bool
	eof         bool
	start       bool
	selectStart bool
	selectEnd   bool
}

func (t *token) eq(o *token) (bool, error) {
	if string(t.buffer) != string(o.buffer) {
		return false, fmt.Errorf("buffer %q != %q", string(t.buffer), string(o.buffer))
	}
	if t.pos != o.pos {
		return false, fmt.Errorf("pos %+v != %+v", t.pos, o.pos)
	}
	if t.rune != nil && o.rune != nil && *t.rune != *o.rune {
		return false, fmt.Errorf("rune %q != %q", string([]rune{*t.rune}), string([]rune{*o.rune}))
	}
	if t.rune != nil && o.rune == nil {
		return false, fmt.Errorf("rune %q != nil", string([]rune{*t.rune}))
	}
	if t.rune == nil && o.rune != nil {
		return false, fmt.Errorf("rune nil != %q", string([]rune{*o.rune}))
	}
	if t.style != nil && o.style != nil && *t.style != *o.style {
		return false, fmt.Errorf("style %+v != %+v", *t.style, *o.style)
	}
	if t.style != nil && o.style == nil {
		return false, fmt.Errorf("style %+v != nil", *t.style)
	}
	if t.style == nil && o.style != nil {
		return false, fmt.Errorf("style nil != %+v", *o.style)
	}
	if t.newLine != o.newLine {
		return false, fmt.Errorf("newLine %v != %v", t.newLine, o.newLine)
	}
	if t.eof != o.eof {
		return false, fmt.Errorf("eof %v != %v", t.eof, o.eof)
	}
	if t.start != o.start {
		return false, fmt.Errorf("start %v != %v", t.start, o.start)
	}
	if t.selectStart != o.selectStart {
		return false, fmt.Errorf("selectStart %v != %v", t.selectStart, o.selectStart)
	}
	if t.selectEnd != o.selectEnd {
		return false, fmt.Errorf("selectEnd %v != %v", t.selectEnd, o.selectEnd)
	}
	return true, nil
}

func (t *token) reset() {
	*t = token{pos: t.pos, buffer: t.buffer}
}

func (t *token) setSelectStart() *token {
	t.reset()
	t.selectStart = true
	return t
}

func (t *token) setSelectEnd() *token {
	t.reset()
	t.selectEnd = true
	return t
}

func (t *token) setStart() *token {
	t.reset()
	t.start = true
	return t
}

func (t *token) setEof() *token {
	t.reset()
	t.eof = true
	return t
}

func (t *token) setNewLine() *token {
	t.reset()
	t.newLine = true
	return t
}

func (t *token) setRune(r rune) *token {
	t.reset()
	t.rune = &r
	return t
}

func (t *token) setStyle(s tcell.Style) *token {
	t.reset()
	t.style = &s
	return t
}

type parseState int

const (
	visible parseState = iota
	escape
	tag
)

func (p parseState) String() string {
	switch p {
	case visible:
		return "visible"
	case escape:
		return "escape"
	case tag:
		return "tag"
	}
	return "unknown"
}

func parseTokens(buffer [][]rune, rawCB func(*token)) {
	state := visible

	inSelection := false

	t := &token{}
	line := []rune{}
	tmpX := 0
	r := rune(0)
	cb := func(t *token) {
		rawCB(t)
		t.buffer = nil
	}

	cb(t.setStart())
	for t.pos.y, line = range buffer {
		for tmpX, r = range line {
			t.buffer = append(t.buffer, r)
			switch state {
			case visible:
				t.pos.x = tmpX
				switch r {
				case '&':
					state = escape
				case '<':
					state = tag
				default:
					cb(t.setRune(r))
				}
			case escape:
				switch r {
				case ';':
					switch string(t.buffer) {
					case "&amp;":
						cb(t.setRune('&'))
						state = visible
					case "&lt;":
						cb(t.setRune('<'))
						state = visible
					case "&gt;":
						cb(t.setRune('>'))
						state = visible
					}
					state = visible
				}
			case tag:
				switch r {
				case '>':
					switch string(t.buffer) {
					case selectFromToken, selectToToken:
						if inSelection {
							cb(t.setSelectEnd())
						} else {
							cb(t.setSelectStart())
						}
						inSelection = !inSelection
					default:
						if match := colorTagPattern.FindStringSubmatch(string(t.buffer)); match != nil {
							fgUint, fgErr := strconv.ParseUint(match[1], 16, 64)
							bgUint, bgErr := strconv.ParseUint(match[2], 16, 64)
							if fgErr == nil && bgErr == nil {
								cb(t.setStyle(tcell.StyleDefault.Foreground(tcell.NewHexColor(int32(fgUint))).Background(tcell.NewHexColor(int32(bgUint)))))
							}
						}
					}
					state = visible
				}
			}
		}
		if t.pos.y+1 < len(buffer) {
			t.pos.x = tmpX + 1
			cb(t.setNewLine())
			state = visible
		}
	}
	t.pos.x = tmpX + 1
	cb(t.setEof())
}

func (e *Editor) redraw() {
	e.screenBuffer = nil
	e.screenBufferIndex = nil

	// No screen makes it impossible to index.
	width, height := e.Screen.Size()
	if width == 0 || height == 0 {
		return
	}

	style := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite)
	selectStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	prevStyle := style
	styleIndex := [][]tcell.Style{}

	beginLine := func() {
		e.screenBuffer = append(e.screenBuffer, nil)
		e.screenBufferIndex = append(e.screenBufferIndex, nil)
		styleIndex = append(styleIndex, nil)
	}
	endLine := func(rawLineIdx int) {
		e.screenBufferIndex[len(e.screenBufferIndex)-1] = append(
			e.screenBufferIndex[len(e.screenBufferIndex)-1],
			point{
				x: -1,
				y: rawLineIdx,
			},
		)
	}

	parseTokens(e.rawBuffer, func(t *token) {
		if t.start {
			beginLine()
		} else if t.newLine {
			endLine(t.pos.y)
			beginLine()
		} else if t.rune != nil {
			e.screenBuffer[len(e.screenBuffer)-1] = append(e.screenBuffer[len(e.screenBuffer)-1], *t.rune)
			e.screenBufferIndex[len(e.screenBufferIndex)-1] = append(e.screenBufferIndex[len(e.screenBufferIndex)-1], t.pos)
			styleIndex[len(styleIndex)-1] = append(styleIndex[len(styleIndex)-1], style)
			if len(e.screenBuffer[len(e.screenBuffer)-1]) > width-1 {
				endLine(t.pos.y)
				beginLine()
			}
		} else if t.style != nil {
			style = *t.style
		} else if t.eof {
			endLine(t.pos.y)
		} else if t.selectStart {
			prevStyle = style
			style = selectStyle
		} else if t.selectEnd {
			style = prevStyle
		}
	})

	for screenLineIdx, screenLine := range e.screenBuffer[e.lineOffset:] {
		for screenRuneIdx, screenRune := range screenLine {
			e.Screen.SetContent(screenRuneIdx, screenLineIdx, screenRune, nil, styleIndex[screenLineIdx+e.lineOffset][screenRuneIdx])
		}
		for x := len(screenLine); x < width; x++ {
			e.Screen.SetContent(x, screenLineIdx, ' ', nil, tcell.StyleDefault)
		}
		if screenLineIdx+1 > height-1 {
			break
		}
	}
	for y := len(e.screenBuffer) - e.lineOffset; y < height; y++ {
		for x := 0; x < width; x++ {
			e.Screen.SetContent(x, y, ' ', nil, tcell.StyleDefault)
		}
	}
}

func stringToRunes(s string) [][]rune {
	res := [][]rune{}
	for _, line := range strings.Split(s, "\n") {
		res = append(res, []rune(line))
	}
	return res
}

func (e *Editor) setRaw(s string) {
	e.rawBuffer = stringToRunes(s)
}

func (e *Editor) Content() string {
	return runesToString(e.rawBuffer)
}

func (e *Editor) SetContent(s string) {
	defer func() {
		e.redraw()
		e.setCursor()
		e.Screen.Show()
	}()
	e.rawBuffer = stringToRunes(s)
}

func (e *Editor) Edit(s string) (string, error) {
	e.differ = diffmatchpatch.New()
	e.rawBuffer = stringToRunes(s)
	e.redraw()
	e.setCursor()
	e.Screen.Show()
	e.pollKeys()
	return "", nil
}
