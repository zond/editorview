package editorview

import (
	"fmt"
	"log"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

var (
	whitespacePattern = regexp.MustCompile("\\s+")
	selectFromToken   = "<select-from>"
	selectFromPattern = regexp.MustCompile(selectFromToken)
	selectToToken     = "<select-to>"
	selectToPattern   = regexp.MustCompile(selectToToken)
	colorTagPattern   = regexp.MustCompile("<color:([A-Fa-f0-9]{6,6}):([A-Fa-f0-9]{6,6})>")
)

func Escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "&", "&amp;"), "<", "&lt;"), ">", "&gt;")
}

type point struct {
	x int
	y int
}

func (p point) clone() *point {
	cpy := p
	return &cpy
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

type Editor struct {
	Screen tcell.Screen

	// Edited text: [line][rune]
	contentBuffer [][]rune
	// Line wrapped edited text: [line][rune]
	wrappedBuffer [][]rune
	// Index from [line][rune] of wrapped edited text to point{x: rune, y: line} of content buffer.
	// Every line has a single point{x: -1, y: lineIdx} appended at the end to mark both cursor
	// positions at the 'newline position' and lineIdx for empty lines.
	wrappedBufferIndex [][]point
	// number of wrappedBuffer lines hidden above screen
	lineOffset int

	selecting bool
	cursor    point
}

func (e *Editor) runeAt(screenPoint point) rune {
	wrappedPoint := point{x: screenPoint.x, y: screenPoint.y + e.lineOffset}
	if wrappedPoint.y < len(e.wrappedBuffer) {
		if wrappedPoint.x < len(e.wrappedBuffer[wrappedPoint.y]) {
			return e.wrappedBuffer[wrappedPoint.y][wrappedPoint.x]
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
	p := e.wrappedBufferIndex[screenY+e.lineOffset][0]
	for x, r := range e.contentBuffer[p.y] {
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
	defer func() {
		e.redraw()
	}()
	p := e.wrappedBufferIndex[screenPoint.y+e.lineOffset][screenPoint.x]
	if p.x < 0 {
		e.contentBuffer = append(
			e.contentBuffer[:p.y+1],
			append(
				[][]rune{nil},
				e.contentBuffer[p.y+1:]...)...)
		return
	}
	e.contentBuffer = append(
		e.contentBuffer[:p.y],
		append(
			[][]rune{e.contentBuffer[p.y][:p.x]},
			append(
				[][]rune{e.contentBuffer[p.y][p.x:]},
				e.contentBuffer[p.y+1:]...)...)...)
}

func (e *Editor) deleteFromContentBuffer(p point) {
	if p.x < 0 {
		if p.y+1 < len(e.contentBuffer) {
			e.contentBuffer = append(
				e.contentBuffer[:p.y],
				append(
					[][]rune{append(e.contentBuffer[p.y], e.contentBuffer[p.y+1]...)},
					e.contentBuffer[p.y+2:]...)...)
		}
		return
	}
	if e.contentBuffer[p.y][p.x] == ';' {
		if p.x > 3 && string(e.contentBuffer[p.y][p.x-4:p.x+1]) == "&amp;" {
			e.contentBuffer[p.y] = append(e.contentBuffer[p.y][:p.x-4], e.contentBuffer[p.y][p.x+1:]...)
			return
		} else if p.x > 3 && string(e.contentBuffer[p.y][p.x-3:p.x+1]) == "&gt;" {
			e.contentBuffer[p.y] = append(e.contentBuffer[p.y][:p.x-3], e.contentBuffer[p.y][p.x+1:]...)
			return
		} else if p.x > 3 && string(e.contentBuffer[p.y][p.x-3:p.x+1]) == "&lt;" {
			e.contentBuffer[p.y] = append(e.contentBuffer[p.y][:p.x-3], e.contentBuffer[p.y][p.x+1:]...)
			return
		}
	}
	e.contentBuffer[p.y] = append(e.contentBuffer[p.y][:p.x], e.contentBuffer[p.y][p.x+1:]...)
}

func (e *Editor) deleteAt(screenPoint point) {
	defer func() {
		e.redraw()
	}()
	e.deleteFromContentBuffer(e.wrappedBufferIndex[screenPoint.y+e.lineOffset][screenPoint.x])
}

func (e *Editor) content(start, end *point) (filtered []rune, matchingRange [2]int, index points) {
	matchingRange = [2]int{-1, -1}
	e.parseTokens(e.contentBuffer, func(t *token) {
		if start != nil && matchingRange[0] != -1 && !(points{*start, t.pos}.Less(0, 1)) {
			matchingRange[0] = len(filtered)
		}
		if end != nil && matchingRange[1] != -1 && !(points{*end, t.pos}.Less(0, 1)) {
			matchingRange[0] = len(filtered)
		}
		if t.rune != nil {
			index = append(index, t.pos)
			filtered = append(filtered, *t.rune)
		} else if t.newLine {
			index = append(index, t.pos)
			filtered = append(filtered, '\n')
		}
	})
	return filtered, matchingRange, index
}

func (e *Editor) replace(raw bool, p *regexp.Regexp, repl string, query func(match string, contentBufferStart, contentBufferEnd point) bool) {
	content := []rune{}
	contentIndex := points{}
	contentOffset := 0
	if raw {
		x := 0
		y := 0
		line := []rune{}
		r := rune(0)
		for y, line = range e.contentBuffer {
			for x, r = range line {
				contentIndex = append(contentIndex, point{x: x, y: y})
				content = append(content, r)
			}
			if y+1 < len(e.contentBuffer) {
				contentIndex = append(contentIndex, point{x: x + 1, y: y})
				content = append(content, '\n')
			}
		}
	} else {
		content, _, contentIndex = e.content(nil, nil)
	}
	for {
		processedContent := content[contentOffset:]
		processedContentIndex := contentIndex[contentOffset:]
		matchIndex := p.FindStringIndex(string(processedContent))
		log.Printf("replace got matchIndex %#v, content has %v runes", matchIndex, len(content))
		if matchIndex == nil {
			return
		}
		// TODO(zond): This goes out of bounds if the match is for the ENTIRE buffer.
		contentStartIndex, contentEndIndex := processedContentIndex[matchIndex[0]], processedContentIndex[matchIndex[1]]
		if query(string(processedContent)[matchIndex[0]:matchIndex[1]], contentStartIndex, contentEndIndex) {
			replacement := p.ReplaceAllString(string(processedContent)[matchIndex[0]:matchIndex[1]], repl)
			if raw {
				content = append(
					content[:contentOffset],
					append(
						processedContent[:matchIndex[0]],
						append(
							[]rune(replacement),
							processedContent[matchIndex[1]:]...)...)...)
				e.setContent(string(content))
			} else {
				for i := matchIndex[1] - 1; i >= matchIndex[0]; i-- {
					p := processedContentIndex[i]
					e.deleteFromContentBuffer(p)
				}
				e.writeAt([]rune(replacement), processedContentIndex[matchIndex[0]])
			}
			contentOffset += matchIndex[1]
			e.redraw()
			if contentOffset > len(content)-1 {
				return
			}
		}
	}
}

func (e *Editor) writeAt(runes []rune, screenPoint point) {
	defer func() {
		e.redraw()
	}()
	p := e.wrappedBufferIndex[screenPoint.y+e.lineOffset][screenPoint.x]
	if p.x < 0 {
		e.contentBuffer[p.y] = append(e.contentBuffer[p.y], runes...)
		return
	}
	e.contentBuffer[p.y] = append(
		e.contentBuffer[p.y][:p.x],
		append(
			runes,
			e.contentBuffer[p.y][p.x:]...)...)
}

func (e *Editor) pollKeys() {
	var selectFrom *point
	for {
		selectFrom = nil
		switch ev := e.Screen.PollEvent().(type) {
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
				if e.moveCursor(left) {
					e.deleteAt(e.cursor)
				}
			case tcell.KeyDelete:
				e.deleteAt(e.cursor)
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
				e.lineOffset = len(e.wrappedBuffer) - height/2
				e.cursor.y = len(e.wrappedBuffer) - e.lineOffset - 1
				e.cursor.x = len(e.wrappedBuffer[e.cursor.y])
				e.redraw()
				e.setCursor()
			case tcell.KeyCtrlC:
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
			}
		}
		// TODO(zond): Selecting somehow hides escape chars like & and < in the buffer.
		if e.selecting {
			if selectFrom == nil {
				e.selecting = false
			} else {
				e.replace(true, selectToPattern, "", func(string, point, point) bool {
					return true
				})
				e.writeAt([]rune(selectToToken), e.cursor)
			}
		} else {
			if selectFrom != nil {
				e.selecting = true
				e.replace(true, selectToPattern, "", func(string, point, point) bool {
					return true
				})
				e.replace(true, selectFromPattern, "", func(string, point, point) bool {
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
		e.Screen.ShowCursor(e.cursor.x, e.cursor.y)
		e.Screen.Show()
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
		return e.lineOffset+1 < len(e.wrappedBuffer)-height/2
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
	e.limitInt(&e.lineOffset, 0, len(e.wrappedBuffer)-height/2)
	e.redraw()
}

func (e *Editor) moveCursor(d direction) bool {
	defer func() {
		e.setCursor()
	}()
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
			e.cursor.x = len(e.wrappedBuffer[e.cursor.y+e.lineOffset])
			return true
		} else if e.canScroll(up) {
			e.scroll(up)
			e.cursor.x = len(e.wrappedBuffer[e.cursor.y+e.lineOffset])
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
		} else if e.cursor.y+e.lineOffset+1 < len(e.wrappedBuffer) && e.canScroll(down) {
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
	if y+e.lineOffset < len(e.wrappedBuffer) {
		return len(e.wrappedBuffer[y+e.lineOffset])
	}
	return 0
}

func (e *Editor) setCursor() {
	width, height := e.Screen.Size()
	if width == 0 || height == 0 {
		return
	}
	e.limitInt(&e.cursor.y, 0, e.minInt(height, len(e.wrappedBuffer)-e.lineOffset))
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
		return e.cursor.y+1 < height && e.cursor.y+e.lineOffset < len(e.wrappedBuffer)-1
	case right:
		return e.cursor.x+1 < width && e.cursor.x < e.lineWidth(e.cursor.y)
	}
	return false
}

type token struct {
	pos         point
	rune        *rune
	style       *tcell.Style
	newLine     bool
	eof         bool
	start       bool
	selectStart bool
	selectEnd   bool
}

func (t *token) reset() {
	*t = token{pos: t.pos}
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

func (e *Editor) parseTokens(buffer [][]rune, cb func(*token)) {
	state := visible
	stateBuffer := []rune{}

	inSelection := false

	token := token{}
	line := []rune{}
	r := rune(0)

	cb(token.setStart())
	for token.pos.y, line = range buffer {
		for token.pos.x, r = range line {
			switch state {
			case visible:
				switch r {
				case '&':
					stateBuffer = []rune{r}
					state = escape
				case '<':
					stateBuffer = []rune{r}
					state = tag
				default:
					cb(token.setRune(r))
				}
			case escape:
				switch r {
				case ';':
					stateBuffer = append(stateBuffer, r)
					switch string(stateBuffer) {
					case "&amp;":
						cb(token.setRune('&'))
						state = visible
					case "&lt;":
						cb(token.setRune('<'))
						state = visible
					case "&gt;":
						cb(token.setRune('>'))
						state = visible
					}
					state = visible
				default:
					stateBuffer = append(stateBuffer, r)
				}
			case tag:
				switch r {
				case '>':
					stateBuffer = append(stateBuffer, r)
					switch string(stateBuffer) {
					case selectFromToken, selectToToken:
						if inSelection {
							cb(token.setSelectEnd())
						} else {
							cb(token.setSelectStart())
						}
						inSelection = !inSelection
					default:
						if match := colorTagPattern.FindStringSubmatch(string(stateBuffer)); match != nil {
							fgUint, fgErr := strconv.ParseUint(match[1], 16, 64)
							bgUint, bgErr := strconv.ParseUint(match[2], 16, 64)
							if fgErr == nil && bgErr == nil {
								cb(token.setStyle(tcell.StyleDefault.Foreground(tcell.NewHexColor(int32(fgUint))).Background(tcell.NewHexColor(int32(bgUint)))))
							}
						}
					}
					state = visible
				default:
					stateBuffer = append(stateBuffer, r)
				}
			}
		}
		if token.pos.y+1 < len(buffer) {
			token.pos.x++
			cb(token.setNewLine())
			state = visible
		}
	}
	token.pos.x++
	cb(token.setEof())
}

func (e *Editor) redraw() {
	e.wrappedBuffer = nil
	e.wrappedBufferIndex = nil

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
		e.wrappedBuffer = append(e.wrappedBuffer, nil)
		e.wrappedBufferIndex = append(e.wrappedBufferIndex, nil)
		styleIndex = append(styleIndex, nil)
	}
	endLine := func(contentLineIdx int) {
		e.wrappedBufferIndex[len(e.wrappedBufferIndex)-1] = append(
			e.wrappedBufferIndex[len(e.wrappedBufferIndex)-1],
			point{
				x: -1,
				y: contentLineIdx,
			},
		)
	}

	e.parseTokens(e.contentBuffer, func(t *token) {
		if t.start {
			beginLine()
		} else if t.newLine {
			endLine(t.pos.y)
			beginLine()
		} else if t.rune != nil {
			e.wrappedBuffer[len(e.wrappedBuffer)-1] = append(e.wrappedBuffer[len(e.wrappedBuffer)-1], *t.rune)
			e.wrappedBufferIndex[len(e.wrappedBufferIndex)-1] = append(e.wrappedBufferIndex[len(e.wrappedBufferIndex)-1], t.pos)
			styleIndex[len(styleIndex)-1] = append(styleIndex[len(styleIndex)-1], style)
			if len(e.wrappedBuffer[len(e.wrappedBuffer)-1]) > width-1 {
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

	for wrappedLineIdx, wrappedLine := range e.wrappedBuffer[e.lineOffset:] {
		for wrappedRuneIdx, wrappedRune := range wrappedLine {
			e.Screen.SetContent(wrappedRuneIdx, wrappedLineIdx, wrappedRune, nil, styleIndex[wrappedLineIdx+e.lineOffset][wrappedRuneIdx])
		}
		for x := len(wrappedLine); x < width; x++ {
			e.Screen.SetContent(x, wrappedLineIdx, ' ', nil, tcell.StyleDefault)
		}
		if wrappedLineIdx+1 > height-1 {
			break
		}
	}
	for y := len(e.wrappedBuffer) - e.lineOffset; y < height; y++ {
		for x := 0; x < width; x++ {
			e.Screen.SetContent(x, y, ' ', nil, tcell.StyleDefault)
		}
	}
}

func (e *Editor) setContent(s string) {
	e.contentBuffer = nil
	for _, line := range strings.Split(s, "\n") {
		e.contentBuffer = append(e.contentBuffer, []rune(line))
	}
}

func (e *Editor) Edit(s string) (string, error) {
	e.setContent(s)
	e.redraw()
	e.setCursor()
	e.Screen.Show()
	e.pollKeys()
	return "", nil
}
