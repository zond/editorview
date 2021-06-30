package editorview

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

var (
	whitespacePattern = regexp.MustCompile("\\s+")
)

func Escape(s string) string {
	return strings.ReplaceAll(s, "\\", "\\\\")
}

type Editor struct {
	Screen tcell.Screen

	// Edited text: [line][rune]
	contentBuffer [][]rune
	// Line wrapped edited text: [line][rune]
	wrappedBuffer [][]rune
	// Index from [line][rune] of wrapped edited text to [rune, line] of content buffer.
	// Every line has a single [2]int{-1, lineIdx} appended at the end to mark both cursor
	// positions at the 'newline position' and lineIdx for empty lines.
	wrappedBufferIndex [][][2]int
	// number of wrappedBuffer lines hidden above screen
	lineOffset int

	cursor struct {
		x int
		y int
	}
}

func (e *Editor) runeAt(x, y int) rune {
	lineIdx := y + e.lineOffset
	if lineIdx < len(e.wrappedBuffer) {
		if x < len(e.wrappedBuffer[lineIdx]) {
			return e.wrappedBuffer[lineIdx][x]
		}
		return '\n'
	}
	return 0
}

func (e *Editor) differentWhitespaceness(x, y int) func(int, int) bool {
	currWhitespaceness := whitespacePattern.MatchString(string([]rune{e.runeAt(x, y)}))
	return func(x, y int) bool {
		return currWhitespaceness != whitespacePattern.MatchString(string([]rune{e.runeAt(x, y)}))
	}
}

func (e *Editor) indentation(y int) int {
	lineIdx := y + e.lineOffset
	idx := e.wrappedBufferIndex[lineIdx][0]
	for x, r := range e.contentBuffer[idx[1]] {
		if whitespacePattern.MatchString(string([]rune{r})) {
			return x
		}
	}
	return 0
}

func (e *Editor) differentIndentness(x, y int) func(int, int) bool {
	currIndentness := e.indentation(y)
	return func(x, y int) bool {
		return currIndentness != e.indentation(y)
	}
}

func (e *Editor) moveCursorUntil(dir direction, cont func(int, int) bool) {
	if cont != nil {
		for e.moveCursor(dir) {
			if cont(e.cursor.x, e.cursor.y) {
				break
			}
		}
	} else {
		e.moveCursor(dir)
	}
}

func (e *Editor) addLineAt(x, y int) {
	defer func() {
		e.redraw()
		e.Screen.Show()
	}()
	lineIdx := y + e.lineOffset
	idx := e.wrappedBufferIndex[lineIdx][x]
	if idx[0] < 0 {
		e.contentBuffer = append(
			e.contentBuffer[:idx[1]+1],
			append(
				[][]rune{nil},
				e.contentBuffer[idx[1]+1:]...)...)
		return
	}
	e.contentBuffer = append(
		e.contentBuffer[:idx[1]],
		append(
			[][]rune{e.contentBuffer[idx[1]][:idx[0]]},
			append(
				[][]rune{e.contentBuffer[idx[1]][idx[0]:]},
				e.contentBuffer[idx[1]+1:]...)...)...)
}

func (e *Editor) deleteAt(x, y int) {
	defer func() {
		e.redraw()
		e.Screen.Show()
	}()
	lineIdx := y + e.lineOffset
	idx := e.wrappedBufferIndex[lineIdx][x]
	if idx[0] < 0 {
		if lineIdx+1 < len(e.wrappedBufferIndex) {
			e.contentBuffer = append(
				e.contentBuffer[:idx[1]],
				append(
					[][]rune{append(e.contentBuffer[idx[1]], e.contentBuffer[idx[1]+1]...)},
					e.contentBuffer[idx[1]+2:]...)...)
		}
		return
	}
	e.contentBuffer[idx[1]] = append(e.contentBuffer[idx[1]][:idx[0]], e.contentBuffer[idx[1]][idx[0]+1:]...)
}

func (e *Editor) writeAt(runes []rune, x, y int) {
	defer func() {
		e.redraw()
		e.Screen.Show()
	}()
	lineIdx := y + e.lineOffset
	idx := e.wrappedBufferIndex[lineIdx][x]
	if idx[0] < 0 {
		e.contentBuffer[idx[1]] = append(e.contentBuffer[idx[1]], runes...)
		return
	}
	e.contentBuffer[idx[1]] = append(
		e.contentBuffer[idx[1]][:idx[0]],
		append(
			runes,
			e.contentBuffer[idx[1]][idx[0]:]...)...)
}

func (e *Editor) pollKeys() {
	for {
		switch ev := e.Screen.PollEvent().(type) {
		case *tcell.EventResize:
			e.redraw()
			e.setCursor()
			e.Screen.Show()
		case *tcell.EventKey:
			switch ev.Key() {
			case tcell.KeyEnter:
				e.addLineAt(e.cursor.x, e.cursor.y)
				e.moveCursor(right)
			case tcell.KeyBackspace:
				e.moveCursor(left)
				whitespaceness := whitespacePattern.MatchString(string([]rune{e.runeAt(e.cursor.x, e.cursor.y)}))
				e.deleteAt(e.cursor.x, e.cursor.y)
				for e.moveCursor(left) {
					if whitespaceness != whitespacePattern.MatchString(string([]rune{e.runeAt(e.cursor.x, e.cursor.y)})) {
						e.moveCursor(right)
						break
					}
					e.deleteAt(e.cursor.x, e.cursor.y)
				}
			case tcell.KeyBackspace2:
				if e.moveCursor(left) {
					e.deleteAt(e.cursor.x, e.cursor.y)
				}
			case tcell.KeyDelete:
				e.deleteAt(e.cursor.x, e.cursor.y)
			case tcell.KeyRune:
				e.writeAt([]rune(Escape(string([]rune{ev.Rune()}))), e.cursor.x, e.cursor.y)
				e.moveCursor(right)
			case tcell.KeyPgUp:
				_, height := e.Screen.Size()
				for i := 0; i < height; i++ {
					if !e.moveCursor(up) {
						break
					}
				}
			case tcell.KeyPgDn:
				_, height := e.Screen.Size()
				for i := 0; i < height; i++ {
					if !e.moveCursor(down) {
						break
					}
				}
			case tcell.KeyHome:
				e.cursor.x = 0
				e.cursor.y = 0
				e.lineOffset = 0
				e.redraw()
				e.setCursor()
				e.Screen.Show()
			case tcell.KeyEnd:
				_, height := e.Screen.Size()
				e.lineOffset = len(e.wrappedBuffer) - height/2
				e.cursor.y = len(e.wrappedBuffer) - e.lineOffset - 1
				e.cursor.x = len(e.wrappedBuffer[e.cursor.y])
				e.redraw()
				e.setCursor()
				e.Screen.Show()
			case tcell.KeyCtrlC:
				e.Screen.Fini()
				return
			case tcell.KeyUp:
				if ev.Modifiers()&tcell.ModCtrl != 0 {
					e.moveCursorUntil(up, e.differentIndentness(e.cursor.x, e.cursor.y))
				} else {
					e.moveCursor(up)
				}
			case tcell.KeyDown:
				if ev.Modifiers()&tcell.ModCtrl != 0 {
					e.moveCursorUntil(down, e.differentIndentness(e.cursor.x, e.cursor.y))
				} else {
					e.moveCursor(down)
				}
			case tcell.KeyLeft:
				if ev.Modifiers()&tcell.ModCtrl != 0 {
					e.moveCursorUntil(left, e.differentWhitespaceness(e.cursor.x, e.cursor.y))
				} else {
					e.moveCursor(left)
				}
			case tcell.KeyRight:
				if ev.Modifiers()&tcell.ModCtrl != 0 {
					e.moveCursorUntil(right, e.differentWhitespaceness(e.cursor.x, e.cursor.y))
				} else {
					e.moveCursor(right)
				}
			}
		}
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
	e.Screen.Show()
}

func (e *Editor) moveCursor(d direction) bool {
	defer func() {
		e.setCursor()
		e.Screen.Show()
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
	e.Screen.ShowCursor(e.cursor.x, e.cursor.y)
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

type parseState int

const (
	visible parseState = iota
	escapeStart
	foreground
	background
)

func (p parseState) String() string {
	switch p {
	case visible:
		return "visible"
	case escapeStart:
		return "escapeStart"
	case foreground:
		return "foreground"
	case background:
		return "background"
	}
	return "unknown"
}

type token struct {
	lineIdx int
	runeIdx int
	rune    *rune
	style   *tcell.Style
	newLine bool
	eof     bool
}

func (t *token) setEof() *token {
	t.rune = nil
	t.style = nil
	t.newLine = false
	t.eof = true
	return t
}

func (t *token) setNewLine() *token {
	t.rune = nil
	t.style = nil
	t.newLine = true
	t.eof = false
	return t
}

func (t *token) setRune(r rune) *token {
	t.rune = &r
	t.style = nil
	t.newLine = false
	t.eof = false
	return t
}

func (t *token) setStyle(s tcell.Style) *token {
	t.rune = nil
	t.style = &s
	t.newLine = false
	t.eof = false
	return t
}

func (e *Editor) parseTokens(buffer [][]rune, cb func(*token)) {
	style := tcell.StyleDefault
	styleBuffer := []rune{}
	setStyle := func(setter func(tcell.Color) tcell.Style) {
		colint, err := strconv.ParseUint(string(styleBuffer), 16, 64)
		styleBuffer = nil
		if err == nil {
			style = setter(tcell.NewHexColor(int32(colint)))
		}
	}
	state := visible
	token := token{}
	line := []rune{}
	r := rune(0)
	for token.lineIdx, line = range buffer {
		cb(token.setNewLine())
		for token.runeIdx, r = range line {
			switch state {
			case visible:
				if r == '\\' {
					state = escapeStart
				} else {
					cb(token.setRune(r))
				}
			case escapeStart:
				if r == '<' {
					state = foreground
				} else {
					cb(token.setRune(r))
					state = visible
				}
			case foreground:
				switch r {
				case ':':
					setStyle(style.Foreground)
					state = background
				case '>':
					setStyle(style.Foreground)
					cb(token.setStyle(style))
					state = visible
				default:
					styleBuffer = append(styleBuffer, r)
				}
			case background:
				if r == '>' {
					setStyle(style.Background)
					cb(token.setStyle(style))
					state = visible
				} else {
					styleBuffer = append(styleBuffer, r)
				}
			}
		}
		state = visible
	}
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

	style := tcell.StyleDefault
	styleIndex := [][]tcell.Style{}

	beginLine := func() {
		e.wrappedBuffer = append(e.wrappedBuffer, nil)
		e.wrappedBufferIndex = append(e.wrappedBufferIndex, nil)
		styleIndex = append(styleIndex, nil)
	}
	endLine := func(contentLineIdx int) {
		e.wrappedBufferIndex[len(e.wrappedBufferIndex)-1] = append(
			e.wrappedBufferIndex[len(e.wrappedBufferIndex)-1],
			[2]int{
				-1,
				contentLineIdx,
			},
		)
	}

	e.parseTokens(e.contentBuffer, func(t *token) {
		if t.newLine {
			if t.lineIdx > 0 {
				endLine(t.lineIdx - 1)
			}
			beginLine()
		} else if t.rune != nil {
			e.wrappedBuffer[len(e.wrappedBuffer)-1] = append(e.wrappedBuffer[len(e.wrappedBuffer)-1], *t.rune)
			e.wrappedBufferIndex[len(e.wrappedBufferIndex)-1] = append(e.wrappedBufferIndex[len(e.wrappedBufferIndex)-1], [2]int{t.runeIdx, t.lineIdx})
			styleIndex[len(styleIndex)-1] = append(styleIndex[len(styleIndex)-1], style)
			if len(e.wrappedBuffer[len(e.wrappedBuffer)-1]) > width-1 {
				endLine(t.lineIdx)
				beginLine()
			}
		} else if t.style != nil {
			style = *t.style
		} else if t.eof {
			endLine(t.lineIdx)
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

func (e *Editor) Edit(s string) (string, error) {
	e.contentBuffer = nil
	for _, line := range strings.Split(s, "\n") {
		e.contentBuffer = append(e.contentBuffer, []rune(line))
	}
	e.redraw()
	e.setCursor()
	e.Screen.Show()
	e.pollKeys()
	return "", nil
}
