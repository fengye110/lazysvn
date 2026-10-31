package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var hunkHeaderRE = regexp.MustCompile(`^@@ -([0-9]+)(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@`)
var pseudoVersionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+-\d{14}-[0-9a-f]{12,}(?:\+incompatible)?(?:\+dirty)?$`)

// version is set at build time via -ldflags "-X main.version=<value>".
var version = "dev"

// diffContextLines 控制 diff 每个 hunk 前后展示的上下文行数。
const diffContextLines = 6

var (
	diffMetaStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	diffHunkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	diffDelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	diffAddStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	diffContextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	diffSepStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	diffHeadStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	diffCloseHint    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type statusEntry struct {
	Code rune
	Path string
	Raw  string
}

type statusMsg struct {
	entries []statusEntry
	raw     string
	err     error
}

type previewMsg struct {
	title string
	body  string
}

type actionMsg struct {
	title   string
	body    string
	refresh bool
}

type changedPath struct {
	Action string
	Kind   string
	Path   string
}

type logEntry struct {
	Revision int64
	Author   string
	Date     time.Time
	Message  string
	Paths    []changedPath
}

type uiMode int

const (
	modeChanges uiMode = iota
	modeLog
)

type uiFocus int

const (
	focusLeft uiFocus = iota
	focusRight
)

type logListMsg struct {
	entries []logEntry
	raw     string
	err     error
}

type logDiffMsg struct {
	revision int64
	path     string
	title    string
	body     string
}

type model struct {
	entries []statusEntry
	cursor  int
	marked  map[string]struct{}

	previewTitle string
	previewBody  string
	statusLine   string

	width  int
	height int

	commitPrompt bool
	commitInput  textinput.Model
	commitPaths  []string

	revertConfirm bool
	revertPaths   []string

	diffSplit bool
	tabFocus  bool

	mode         uiMode
	logEntries   []logEntry
	logCursor    int
	logFocus     uiFocus
	logPaths     []changedPath
	logPathsRev  int64
	pathCursor   int
	logDiff      string
	logDiffTitle string
	prevTitle    string
	prevBody     string
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			fmt.Println(cliHelp())
			return
		case "-v", "--version", "version":
			fmt.Println(currentVersion())
			return
		case "update":
			if len(os.Args) > 3 {
				fmt.Fprintf(os.Stderr, "too many arguments for update\n\n%s\n", cliHelp())
				os.Exit(2)
			}
			if len(os.Args) == 3 {
				switch os.Args[2] {
				case "-h", "--help", "help":
					fmt.Println(updateHelp())
					return
				}
			}

			version := "latest"
			if len(os.Args) == 3 {
				version = strings.TrimSpace(os.Args[2])
				if version == "" {
					version = "latest"
				}
			}

			if err := runSelfUpdate(version); err != nil {
				fmt.Fprintf(os.Stderr, "lazysvn update failed: %v\n", err)
				os.Exit(1)
			}
			return
		case "uninstall":
			if len(os.Args) > 3 {
				fmt.Fprintf(os.Stderr, "too many arguments for uninstall\n\n%s\n", cliHelp())
				os.Exit(2)
			}
			if len(os.Args) == 3 {
				switch os.Args[2] {
				case "-h", "--help", "help":
					fmt.Println(uninstallHelp())
					return
				default:
					fmt.Fprintf(os.Stderr, "unknown argument for uninstall: %s\n\n%s\n", os.Args[2], uninstallHelp())
					os.Exit(2)
				}
			}

			if err := runSelfUninstall(); err != nil {
				fmt.Fprintf(os.Stderr, "lazysvn uninstall failed: %v\n", err)
				os.Exit(1)
			}
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown argument: %s\n\n%s\n", os.Args[1], cliHelp())
			os.Exit(2)
		}
	}

	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "lazysvn failed: %v\n", err)
		os.Exit(1)
	}
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "提交信息"
	ti.Prompt = "> "
	ti.CharLimit = 200

	return model{
		marked:       map[string]struct{}{},
		previewTitle: "帮助",
		previewBody:  helpText(),
		statusLine:   "正在加载 SVN 状态...",
		commitInput:  ti,
		diffSplit:    true,
	}
}

func (m model) Init() tea.Cmd {
	return fetchStatusCmd()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case statusMsg:
		if msg.err != nil {
			m.statusLine = "svn status 执行失败。"
			m.previewTitle = "SVN 错误"
			m.previewBody = formatSVNResult([]string{"status"}, msg.raw, msg.err)
			return m, nil
		}

		m.entries = msg.entries
		m.cleanupMarks()
		if len(m.entries) == 0 {
			m.cursor = 0
			m.statusLine = "工作副本干净。"
			return m, nil
		}
		if m.cursor >= len(m.entries) {
			m.cursor = len(m.entries) - 1
		}
		m.statusLine = fmt.Sprintf("%d 个变更。", len(m.entries))
		// 有变更时右侧直接预览当前文件的 diff，不显示初始 Help
		if m.mode == modeChanges && m.previewTitle == "帮助" {
			if path := m.currentPath(); path != "" {
				m.statusLine = "正在加载差异..."
				return m, diffCmd(path)
			}
		}
		return m, nil

	case previewMsg:
		m.previewTitle = msg.title
		m.previewBody = msg.body
		return m, nil

	case logListMsg:
		if msg.err != nil {
			if m.mode == modeLog {
				m.mode = modeChanges
				m.previewTitle = m.prevTitle
				m.previewBody = m.prevBody
			}
			m.statusLine = "日志加载失败。"
			m.previewTitle = "SVN 错误"
			m.previewBody = formatSVNResult([]string{"log", "--xml", "-v", "-l", "20", "."}, msg.raw, msg.err)
			return m, nil
		}
		m.mode = modeLog
		m.logEntries = msg.entries
		m.logCursor = 0
		m.logFocus = focusLeft
		m.logDiff = ""
		m.logDiffTitle = ""
		m.selectLogRevision()
		if len(msg.entries) == 0 {
			m.statusLine = "没有日志记录。"
		} else {
			m.statusLine = fmt.Sprintf("%d 条日志。", len(msg.entries))
		}
		return m, nil

	case logDiffMsg:
		m.logDiffTitle = msg.title
		m.logDiff = msg.body
		m.statusLine = fmt.Sprintf("已加载 r%d 的差异。", msg.revision)
		return m, nil

	case actionMsg:
		m.previewTitle = msg.title
		m.previewBody = msg.body
		if strings.Contains(strings.ToLower(msg.body), "error") {
			m.statusLine = "操作失败。"
		} else {
			m.statusLine = "操作完成。"
		}
		if msg.refresh {
			return m, fetchStatusCmd()
		}
		return m, nil

	case tea.KeyMsg:
		if m.commitPrompt {
			switch msg.String() {
			case "esc":
				m.commitPrompt = false
				m.commitInput.Blur()
				m.statusLine = "已取消提交。"
				return m, nil
			case "enter":
				message := strings.TrimSpace(m.commitInput.Value())
				if message == "" {
					m.statusLine = "提交信息不能为空。"
					return m, nil
				}
				paths := append([]string(nil), m.commitPaths...)
				m.commitPrompt = false
				m.commitInput.Blur()
				m.statusLine = "正在执行 svn commit..."
				return m, commitCmd(paths, message)
			default:
				var cmd tea.Cmd
				m.commitInput, cmd = m.commitInput.Update(msg)
				return m, cmd
			}
		}

		if m.revertConfirm {
			switch msg.String() {
			case "y":
				paths := append([]string(nil), m.revertPaths...)
				m.revertConfirm = false
				m.statusLine = "正在执行 svn revert..."
				return m, revertCmd(paths)
			case "n", "esc":
				m.revertConfirm = false
				m.statusLine = "已取消还原。"
				return m, nil
			}
			return m, nil
		}

		if m.tabFocus {
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "left", "h":
				return m, m.switchTab(false)
			case "right", "l":
				return m, m.switchTab(true)
			case "enter", "down", "esc":
				m.tabFocus = false
				return m, nil
			}
			return m, nil
		}

		if m.mode == modeLog {
			return m.handleLogKey(msg)
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up":
			if m.cursor == 0 {
				m.tabFocus = true
				return m, nil
			}
			m.moveCursor(-1)
			return m, m.maybeFollowDiff()
		case "k":
			m.moveCursor(-1)
			return m, m.maybeFollowDiff()
		case "down", "j":
			m.moveCursor(1)
			return m, m.maybeFollowDiff()
		case "g", "home":
			m.cursor = 0
			return m, m.maybeFollowDiff()
		case "G", "end":
			if len(m.entries) > 0 {
				m.cursor = len(m.entries) - 1
			}
			return m, m.maybeFollowDiff()
		case " ":
			m.toggleMark()
			return m, nil
		case "r":
			m.statusLine = "刷新中..."
			return m, fetchStatusCmd()
		case "enter":
			m.toggleMark()
			return m, nil
		case "d":
			path := m.currentPath()
			if path == "" {
				m.statusLine = "未选择文件。"
				return m, nil
			}
			m.statusLine = "正在加载差异..."
			return m, diffCmd(path)
		case "l":
			path := m.currentPath()
			if path == "" {
				m.statusLine = "未选择文件。"
				return m, nil
			}
			m.statusLine = "正在加载日志..."
			return m, logCmd(path)
		case "L":
			return m, m.openLogTab(true)
		case "tab":
			return m, m.openLogTab(false)
		case "C":
			return m, m.switchTab(false)
		case "u":
			m.statusLine = "正在执行 svn update..."
			return m, updateCmd()
		case "a":
			targets := m.addTargets()
			if len(targets) == 0 {
				m.statusLine = "请选择状态为 '?' 的未版本化文件。"
				return m, nil
			}
			m.statusLine = "正在执行 svn add..."
			return m, addCmd(targets)
		case "v":
			targets := m.selectedTargets()
			if len(targets) == 0 {
				m.statusLine = "未选择文件。"
				return m, nil
			}
			m.revertConfirm = true
			m.revertPaths = targets
			return m, nil
		case "c":
			m.commitPaths = m.commitTargets()
			m.commitPrompt = true
			m.commitInput.SetValue("")
			m.commitInput.Focus()
			m.statusLine = fmt.Sprintf("提交 %d 个路径，请输入提交信息。", len(m.commitPaths))
			return m, nil
		case "h", "?":
			m.previewTitle = "帮助"
			m.previewBody = helpText()
			return m, nil
		case "+":
			if strings.HasPrefix(m.previewTitle, "差异:") {
				m.diffSplit = !m.diffSplit
				if m.diffSplit {
					m.statusLine = "差异视图：左右对比。"
				} else {
					m.statusLine = "差异视图：统一格式。"
				}
			}
			return m, nil
		}
	}

	return m, nil
}

func (m model) handleLogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		if m.logFocus == focusRight {
			// 从文件列表回到日志列表（恢复 Log 在左、Files 在右）
			m.logFocus = focusLeft
			m.logDiff = ""
			m.logDiffTitle = ""
			m.statusLine = "已返回日志列表。"
			return m, nil
		}
		m.exitLogMode()
		return m, nil
	case "tab":
		// Tab 只用于在两个 tab 之间切换，不再切换日志内的左右焦点
		return m, m.switchTab(false)
	case "left":
		if m.logFocus == focusRight {
			m.logFocus = focusLeft
			m.logDiff = ""
			m.logDiffTitle = ""
			m.statusLine = "已返回日志列表。"
		}
		return m, nil
	case "right":
		if m.logFocus == focusLeft && len(m.logPaths) > 0 {
			m.logFocus = focusRight
			m.statusLine = "正在加载差异..."
			return m, m.logFilePreviewCmd()
		}
		return m, nil
	case "up":
		if m.logFocus == focusRight {
			if m.pathCursor == 0 {
				m.tabFocus = true
				return m, nil
			}
			m.pathCursor--
			m.statusLine = "正在加载差异..."
			return m, m.logFilePreviewCmd()
		}
		if m.logCursor == 0 {
			m.tabFocus = true
			return m, nil
		}
		m.logCursor--
		m.selectLogRevision()
		return m, nil
	case "k":
		if m.logFocus == focusRight {
			if m.pathCursor > 0 {
				m.pathCursor--
			}
			m.statusLine = "正在加载差异..."
			return m, m.logFilePreviewCmd()
		}
		if m.logCursor > 0 {
			m.logCursor--
			m.selectLogRevision()
		}
		return m, nil
	case "down", "j":
		if m.logFocus == focusRight {
			if m.pathCursor < len(m.logPaths)-1 {
				m.pathCursor++
			}
			m.statusLine = "正在加载差异..."
			return m, m.logFilePreviewCmd()
		}
		if m.logCursor < len(m.logEntries)-1 {
			m.logCursor++
			m.selectLogRevision()
		}
		return m, nil
	case "g", "home":
		if m.logFocus == focusRight {
			m.pathCursor = 0
			m.statusLine = "正在加载差异..."
			return m, m.logFilePreviewCmd()
		}
		m.logCursor = 0
		m.selectLogRevision()
		return m, nil
	case "G", "end":
		if m.logFocus == focusRight {
			if len(m.logPaths) > 0 {
				m.pathCursor = len(m.logPaths) - 1
			}
			m.statusLine = "正在加载差异..."
			return m, m.logFilePreviewCmd()
		}
		if len(m.logEntries) > 0 {
			m.logCursor = len(m.logEntries) - 1
			m.selectLogRevision()
		}
		return m, nil
	case "enter":
		if m.logFocus == focusRight {
			if len(m.logPaths) == 0 {
				m.statusLine = "未选择文件。"
				return m, nil
			}
			m.statusLine = "正在加载差异..."
			return m, m.logFilePreviewCmd()
		}
		m.selectLogRevision()
		if len(m.logPaths) > 0 {
			// 选中版本的文件列表就绪：交换布局并把焦点交给 Files 列表
			m.logFocus = focusRight
			m.statusLine = "正在加载差异..."
			return m, m.logFilePreviewCmd()
		}
		m.statusLine = fmt.Sprintf("r%d 没有变更文件。", m.logPathsRev)
		return m, nil
	case "L":
		return m, m.openLogTab(true)
	case "C":
		return m, m.switchTab(false)
	case "+":
		m.diffSplit = !m.diffSplit
		if m.diffSplit {
			m.statusLine = "差异视图：左右对比。"
		} else {
			m.statusLine = "差异视图：统一格式。"
		}
		return m, nil
	}
	return m, nil
}

func (m model) logFilePreviewCmd() tea.Cmd {
	if len(m.logPaths) == 0 {
		return nil
	}
	return logFileDiffCmd(m.logPathsRev, m.logPaths[m.pathCursor].Path)
}

func (m *model) openLogTab(reload bool) tea.Cmd {
	if m.mode != modeLog {
		m.prevTitle = m.previewTitle
		m.prevBody = m.previewBody
	}
	m.mode = modeLog

	if !reload && len(m.logEntries) > 0 {
		// 复用缓存：保留上次的版本/文件光标、布局与 diff
		m.statusLine = fmt.Sprintf("%d 条日志。", len(m.logEntries))
		return nil
	}

	m.logEntries = nil
	m.logCursor = 0
	m.logFocus = focusLeft
	m.logPaths = nil
	m.logPathsRev = 0
	m.pathCursor = 0
	m.logDiff = ""
	m.logDiffTitle = ""
	m.statusLine = "正在加载日志..."
	return fetchLogCmd()
}

func (m *model) switchTab(toLog bool) tea.Cmd {
	if toLog {
		if m.mode == modeLog {
			return nil
		}
		return m.openLogTab(false)
	}
	m.exitLogMode()
	return nil
}

func (m *model) selectLogRevision() {
	m.logPaths = nil
	m.logPathsRev = 0
	m.pathCursor = 0

	if m.logCursor < 0 || m.logCursor >= len(m.logEntries) {
		return
	}
	entry := m.logEntries[m.logCursor]
	m.logPaths = entry.Paths
	m.logPathsRev = entry.Revision
}

func (m *model) exitLogMode() {
	if m.mode != modeLog {
		return
	}
	// 只切回 Changes tab，保留日志缓存（版本/文件光标、布局、diff），
	// 下次进入 SVN log 时恢复上次位置。
	m.mode = modeChanges
	m.previewTitle = m.prevTitle
	m.previewBody = m.prevBody
	m.tabFocus = false
	if len(m.entries) == 0 {
		m.statusLine = "工作副本干净。"
	} else {
		m.statusLine = fmt.Sprintf("%d 个变更。", len(m.entries))
	}
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "加载中..."
	}

	tabs := m.renderTabs(m.width)
	bodyH := m.height - 4
	if bodyH < 6 {
		bodyH = 6
	}

	frameW := paneStyle().GetHorizontalFrameSize()
	frameH := paneStyle().GetVerticalFrameSize()
	contentW := max(8, m.width-frameW)
	body := ""

	// Use a stacked layout in narrower terminals to avoid right-edge clipping.
	if m.width < 110 {
		topOuterH := bodyH * 45 / 100
		if topOuterH < 4 {
			topOuterH = bodyH / 2
		}
		bottomOuterH := bodyH - topOuterH
		top := m.renderLeftPane(contentW, max(3, topOuterH-frameH))
		bottom := m.renderRightPane(contentW, max(3, bottomOuterH-frameH))
		body = lipgloss.JoinVertical(lipgloss.Left, top, bottom)
	} else {
		leftTotalW := m.width * 45 / 100
		if leftTotalW < 40 {
			leftTotalW = m.width / 2
		}
		rightTotalW := m.width - leftTotalW
		left := m.renderLeftPane(max(8, leftTotalW-frameW), max(3, bodyH-frameH))
		right := m.renderRightPane(max(8, rightTotalW-frameW), max(3, bodyH-frameH))
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	footer := m.renderFooter(m.width)
	return strings.Join([]string{tabs, body, footer}, "\n")
}

func paneInnerWidth(contentWidth int) int {
	// lipgloss 的 Width 包含左右 padding（各 1 列）
	return max(8, contentWidth-2)
}

func renderPaneTitle(title, hint string, width int) string {
	if hint == "" {
		return truncate(title, width)
	}
	styled := diffCloseHint.Render(hint)
	// 左侧标题、右侧提示；放不下时退化为只显示标题
	if gap := width - ansi.StringWidth(title) - ansi.StringWidth(styled); gap >= 1 {
		return title + strings.Repeat(" ", gap) + styled
	}
	return truncate(title, width)
}

func renderPaneTitleMid(title, mid, hint string, width int) string {
	if mid == "" {
		return renderPaneTitle(title, hint, width)
	}
	styledMid := diffCloseHint.Render(mid)
	styledHint := diffCloseHint.Render(hint)
	midW := ansi.StringWidth(styledMid)
	hintW := ansi.StringWidth(styledHint)

	// 为 mid 两侧各留 2 列间距；空间不足时截断标题，保证提示始终可见
	maxTitleW := width - hintW - midW - 5
	if maxTitleW < 8 {
		return renderPaneTitle(title, hint, width)
	}
	if ansi.StringWidth(title) > maxTitleW {
		title = truncate(title, maxTitleW)
	}

	avail := width - ansi.StringWidth(title) - hintW
	leftGap := (avail - midW) / 2
	if leftGap < 2 {
		leftGap = 2
	}
	rightGap := avail - leftGap - midW
	if rightGap < 2 {
		rightGap = 2
	}
	return title + strings.Repeat(" ", leftGap) + styledMid + strings.Repeat(" ", rightGap) + styledHint
}

func (m model) renderTabs(width int) string {
	active := lipgloss.NewStyle().
		Background(lipgloss.Color("11")).
		Foreground(lipgloss.Color("0")).
		Bold(true).
		Padding(0, 1)
	inactive := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		Padding(0, 1)
	if m.tabFocus {
		// tab 栏获得焦点时用蓝色背景区分，提示 ←/→ 可切换
		active = active.Background(lipgloss.Color("12")).Foreground(lipgloss.Color("15"))
	}

	changes := inactive.Render("变化(C)")
	logTab := inactive.Render("SVN 日志(L)")
	if m.mode == modeChanges {
		changes = active.Render("变化(C)")
	} else {
		logTab = active.Render("SVN 日志(L)")
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, changes, logTab)
}

func paneStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(0, 1)
}

func focusedPaneStyle(focused bool) lipgloss.Style {
	style := paneStyle()
	if focused {
		// 聚焦窗口边框用亮黄色区分
		style = style.BorderForeground(lipgloss.Color("11"))
	}
	return style
}

func (m model) renderLeftPane(contentWidth, contentHeight int) string {
	if m.mode == modeLog {
		// 焦点在文件列表时布局交换：Files 在左，Diff 在右
		if m.logFocus == focusRight {
			return m.renderLogFilesPane(contentWidth, contentHeight, !m.tabFocus)
		}
		return m.renderLogPane(contentWidth, contentHeight)
	}
	return m.renderChangesPane(contentWidth, contentHeight)
}

func (m model) renderRightPane(contentWidth, contentHeight int) string {
	if m.mode == modeLog {
		if m.logFocus == focusRight {
			return m.renderLogDiffPane(contentWidth, contentHeight)
		}
		return m.renderLogFilesPane(contentWidth, contentHeight, false)
	}
	return m.renderPreviewPane(contentWidth, contentHeight)
}

func (m model) renderLogPane(contentWidth, contentHeight int) string {
	border := focusedPaneStyle(!m.tabFocus && m.logFocus == focusLeft).
		Width(contentWidth).
		Height(contentHeight)

	innerWidth := paneInnerWidth(contentWidth)
	rows := max(1, contentHeight)

	title := fmt.Sprintf("SVN 日志 (%d)", len(m.logEntries))
	if m.logFocus == focusLeft {
		title += " [焦点]"
	}
	titleLine := title
	if len(m.logPaths) > 0 {
		titleLine += "  " + diffCloseHint.Render("→ 右键切换到右侧")
	}
	lines := []string{renderPaneTitle(titleLine, "Esc 退出日志模式", innerWidth)}
	if len(m.logEntries) == 0 {
		lines = append(lines, "（无日志）")
		return border.Render(strings.Join(lines, "\n"))
	}

	start := windowStart(m.logCursor, len(m.logEntries), rows-1)
	end := min(len(m.logEntries), start+rows-1)
	for i := start; i < end; i++ {
		entry := m.logEntries[i]
		cursor := " "
		if i == m.logCursor {
			cursor = ">"
		}
		line := fmt.Sprintf("%s r%d %s %s  %s", cursor, entry.Revision, formatLogDate(entry.Date), entry.Author, firstLine(entry.Message))
		lines = append(lines, truncate(line, innerWidth))
	}

	return border.Render(strings.Join(lines, "\n"))
}

func (m model) renderLogFilesPane(contentWidth, contentHeight int, focused bool) string {
	border := focusedPaneStyle(focused).
		Width(contentWidth).
		Height(contentHeight)

	innerWidth := paneInnerWidth(contentWidth)
	rows := max(1, contentHeight)

	title := "文件"
	if m.logPathsRev != 0 {
		title = fmt.Sprintf("文件: r%d", m.logPathsRev)
	}
	if focused {
		title += " [焦点]"
	}
	hint := ""
	mid := ""
	titleLine := title
	if len(m.logPaths) > 0 {
		if focused {
			// Files 已在左侧并获得焦点：← 返回日志列表
			mid = "↑↓ 预览"
			hint = "回车查看文件差异"
			titleLine += "  " + diffCloseHint.Render("← 返回日志")
		} else {
			// Files 在右侧：→ 或回车进入文件列表
			mid = "→ 进入文件列表"
			hint = "回车查看文件差异"
		}
	}
	lines := []string{renderPaneTitleMid(titleLine, mid, hint, innerWidth)}

	if len(m.logPaths) == 0 {
		if m.logPathsRev == 0 {
			lines = append(lines, "回车查看该版本的文件列表。")
		} else {
			lines = append(lines, "（无变更文件）")
		}
		return border.Render(strings.Join(lines, "\n"))
	}

	start := windowStart(m.pathCursor, len(m.logPaths), rows-1)
	end := min(len(m.logPaths), start+rows-1)
	for i := start; i < end; i++ {
		p := m.logPaths[i]
		cursor := " "
		if i == m.pathCursor {
			cursor = ">"
		}
		action := "-"
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
		if p.Action != "" {
			action = p.Action
			style = statusCodeStyle(rune(p.Action[0]))
		}
		path := p.Path
		if p.Kind == "dir" && !strings.HasSuffix(path, "/") {
			// 目录末尾加 /，便于与文件区分
			path += "/"
		}
		line := fmt.Sprintf("%s [%s] %s", cursor, style.Render(action), path)
		lines = append(lines, truncate(line, innerWidth))
	}

	return border.Render(strings.Join(lines, "\n"))
}

func (m model) renderLogDiffPane(contentWidth, contentHeight int) string {
	border := focusedPaneStyle(false).
		Width(contentWidth).
		Height(contentHeight)

	innerWidth := paneInnerWidth(contentWidth)
	rows := max(1, contentHeight)

	title := m.logDiffTitle
	if title == "" {
		title = "差异"
	}
	body := m.logDiff
	if body == "" {
		body = "正在加载差异..."
	}
	lines := []string{renderPaneTitleMid(title, "+ 切换差异方式", "Esc 返回日志列表", innerWidth)}
	if m.diffSplit {
		lines = append(lines, renderSideBySideDiff(body, innerWidth)...)
	} else {
		lines = append(lines, renderUnifiedDiff(body, innerWidth)...)
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}

	return border.Render(strings.Join(lines, "\n"))
}

func (m model) renderChangesPane(contentWidth, contentHeight int) string {
	border := focusedPaneStyle(!m.tabFocus).
		Width(contentWidth).
		Height(contentHeight)

	innerWidth := paneInnerWidth(contentWidth)
	rows := max(1, contentHeight)

	mid := ""
	if len(m.entries) > 0 {
		mid = "回车或者空格选中"
	}
	lines := []string{renderPaneTitleMid("变化", mid, "选中待操作文件后操作", innerWidth)}
	if len(m.entries) == 0 {
		lines = append(lines, "工作副本干净。")
		return border.Render(strings.Join(lines, "\n"))
	}

	start := m.scrollStart(rows - 1)
	end := min(len(m.entries), start+rows-1)
	for i := start; i < end; i++ {
		entry := m.entries[i]
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}
		mark := " "
		if _, ok := m.marked[entry.Path]; ok {
			mark = "*"
		}
		code := statusCodeStyle(entry.Code).Render(string(entry.Code))
		line := fmt.Sprintf("%s%s [%s] %s", cursor, mark, code, entry.Path)
		lines = append(lines, truncate(line, innerWidth))
	}

	return border.Render(strings.Join(lines, "\n"))
}

func (m model) renderPreviewPane(contentWidth, contentHeight int) string {
	border := focusedPaneStyle(false).
		Width(contentWidth).
		Height(contentHeight)

	innerWidth := paneInnerWidth(contentWidth)
	rows := max(1, contentHeight)

	var lines []string
	if strings.HasPrefix(m.previewTitle, "差异:") {
		lines = []string{renderPaneTitle(m.previewTitle, "+ 切换差异方式", innerWidth)}
		if m.diffSplit {
			lines = append(lines, renderSideBySideDiff(m.previewBody, innerWidth)...)
		} else {
			lines = append(lines, renderUnifiedDiff(m.previewBody, innerWidth)...)
		}
	} else {
		lines = []string{truncate(m.previewTitle, innerWidth)}
		for _, line := range strings.Split(strings.ReplaceAll(m.previewBody, "\r\n", "\n"), "\n") {
			lines = append(lines, truncate(line, innerWidth))
		}
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}

	return border.Render(strings.Join(lines, "\n"))
}

type diffLine struct {
	no   int
	text string
}

func renderUnifiedDiff(body string, width int) []string {
	raw := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if len(raw) > 0 && strings.HasPrefix(raw[0], "$ svn ") {
		raw = raw[1:]
		for len(raw) > 0 && strings.TrimSpace(raw[0]) == "" {
			raw = raw[1:]
		}
	}
	if len(raw) == 0 {
		return []string{"（无差异输出）"}
	}

	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.ReplaceAll(line, "\t", "    ")
		lines = append(lines, colorizeDiffLine(truncate(line, width)))
	}
	return lines
}

func renderSideBySideDiff(body string, width int) []string {
	raw := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if len(raw) == 0 {
		return []string{"（无差异输出）"}
	}

	if strings.HasPrefix(raw[0], "$ svn ") {
		raw = raw[1:]
		for len(raw) > 0 && strings.TrimSpace(raw[0]) == "" {
			raw = raw[1:]
		}
	}
	if len(raw) == 0 {
		return []string{"（无差异输出）"}
	}
	if !hasUnifiedHunk(raw) {
		return colorizeAndTruncateLines(raw, width)
	}

	separator := " | "
	leftW := (width - len(separator)) / 2
	rightW := width - len(separator) - leftW
	if leftW < 18 || rightW < 18 {
		return colorizeAndTruncateLines(raw, width)
	}

	lines := []string{
		diffHeadStyle.Render(formatDiffRow(leftW, rightW, separator, 0, "旧", ' ', 0, "新", ' ')),
		diffSepStyle.Render(truncate(strings.Repeat("-", leftW)+separator+strings.Repeat("-", rightW), width)),
	}

	var oldLine, newLine int
	var pendingDel []diffLine
	var pendingAdd []diffLine
	sawHunk := false

	flush := func() {
		count := max(len(pendingDel), len(pendingAdd))
		for i := 0; i < count; i++ {
			leftNo, leftText, leftSign := 0, "", ' '
			rightNo, rightText, rightSign := 0, "", ' '
			if i < len(pendingDel) {
				leftNo = pendingDel[i].no
				leftText = pendingDel[i].text
				leftSign = '-'
			}
			if i < len(pendingAdd) {
				rightNo = pendingAdd[i].no
				rightText = pendingAdd[i].text
				rightSign = '+'
			}
			lines = append(lines, formatDiffRow(leftW, rightW, separator, leftNo, leftText, leftSign, rightNo, rightText, rightSign))
		}
		pendingDel = pendingDel[:0]
		pendingAdd = pendingAdd[:0]
	}

	for _, line := range raw {
		switch {
		case strings.HasPrefix(line, "@@"):
			flush()
			if oldStart, newStart, ok := parseHunkStarts(line); ok {
				oldLine = oldStart
				newLine = newStart
				sawHunk = true
			}
			lines = append(lines, diffHunkStyle.Render(truncate("  "+line, width)))
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			if !sawHunk {
				flush()
				lines = append(lines, colorizeDiffLine(truncate(line, width)))
				continue
			}
			pendingDel = append(pendingDel, diffLine{no: oldLine, text: strings.TrimPrefix(line, "-")})
			oldLine++
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if !sawHunk {
				flush()
				lines = append(lines, colorizeDiffLine(truncate(line, width)))
				continue
			}
			pendingAdd = append(pendingAdd, diffLine{no: newLine, text: strings.TrimPrefix(line, "+")})
			newLine++
		case strings.HasPrefix(line, " "):
			if !sawHunk {
				flush()
				lines = append(lines, colorizeDiffLine(truncate(line, width)))
				continue
			}
			flush()
			text := strings.TrimPrefix(line, " ")
			lines = append(lines, formatDiffRow(leftW, rightW, separator, oldLine, text, ' ', newLine, text, ' '))
			oldLine++
			newLine++
		default:
			flush()
			lines = append(lines, colorizeDiffLine(truncate(line, width)))
		}
	}
	flush()

	return lines
}

func hasUnifiedHunk(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			return true
		}
	}
	return false
}

func truncateLines(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, truncate(line, width))
	}
	return out
}

func colorizeAndTruncateLines(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, colorizeDiffLine(truncate(line, width)))
	}
	return out
}

func parseHunkStarts(header string) (int, int, bool) {
	match := hunkHeaderRE.FindStringSubmatch(header)
	if len(match) != 3 {
		return 0, 0, false
	}

	oldStart, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, 0, false
	}
	newStart, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, 0, false
	}
	return oldStart, newStart, true
}

func formatDiffRow(leftW, rightW int, separator string, leftNo int, leftText string, leftSign rune, rightNo int, rightText string, rightSign rune) string {
	left := styleDiffCell(formatDiffCell(leftW, leftNo, leftText, leftSign), leftSign)
	right := styleDiffCell(formatDiffCell(rightW, rightNo, rightText, rightSign), rightSign)
	return left + diffSepStyle.Render(separator) + right
}

func formatDiffCell(width int, lineNo int, text string, sign rune) string {
	lineLabel := ""
	if lineNo > 0 {
		lineLabel = strconv.Itoa(lineNo)
	}
	// 制表符按 4 空格展开，否则实际显示宽度与 ansi.StringWidth 计算不一致，
	// 会把左右对比的中间分隔线推偏。
	text = strings.ReplaceAll(text, "\t", "    ")
	cell := fmt.Sprintf("%5s %c %s", lineLabel, sign, text)
	return padRight(truncate(cell, width), width)
}

func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	// 按终端显示宽度补齐（中文占 2 列）
	w := ansi.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func styleDiffCell(cell string, sign rune) string {
	switch sign {
	case '+':
		return diffAddStyle.Render(cell)
	case '-':
		return diffDelStyle.Render(cell)
	default:
		return diffContextStyle.Render(cell)
	}
}

func colorizeDiffLine(line string) string {
	switch {
	case strings.HasPrefix(line, "@@"):
		return diffHunkStyle.Render(line)
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		return diffAddStyle.Render(line)
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		return diffDelStyle.Render(line)
	case strings.HasPrefix(line, "Index:"),
		strings.HasPrefix(line, "==="),
		strings.HasPrefix(line, "---"),
		strings.HasPrefix(line, "+++"),
		strings.HasPrefix(line, "$ svn"):
		return diffMetaStyle.Render(line)
	default:
		return diffContextStyle.Render(line)
	}
}

func (m model) renderFooter(width int) string {
	help := "j/k 移动  空格 标记  d 差异  l 日志  h 帮助  c 提交  a 添加  v 还原  u 更新  r 刷新  q 退出"
	if m.commitPrompt {
		text := truncate(fmt.Sprintf("提交（%d 个路径）：%s", len(m.commitPaths), m.commitInput.View()), max(8, width))
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).
			Render(text)
	}
	if m.revertConfirm {
		text := truncate(fmt.Sprintf("还原 %d 个路径？按 y 或 n。", len(m.revertPaths)), max(8, width))
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).
			Render(text)
	}
	text := truncate(fmt.Sprintf("%s | %s", help, m.statusLine), max(8, width))
	return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
		Render(text)
}

func (m *model) moveCursor(delta int) {
	if len(m.entries) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}
}

func (m *model) toggleMark() {
	path := m.currentPath()
	if path == "" {
		return
	}
	if _, ok := m.marked[path]; ok {
		delete(m.marked, path)
		return
	}
	m.marked[path] = struct{}{}
}

func (m model) currentPath() string {
	if len(m.entries) == 0 || m.cursor < 0 || m.cursor >= len(m.entries) {
		return ""
	}
	return m.entries[m.cursor].Path
}

func (m *model) cleanupMarks() {
	if len(m.marked) == 0 {
		return
	}
	active := map[string]struct{}{}
	for _, e := range m.entries {
		active[e.Path] = struct{}{}
	}
	for p := range m.marked {
		if _, ok := active[p]; !ok {
			delete(m.marked, p)
		}
	}
}

func (m model) selectedTargets() []string {
	if len(m.marked) > 0 {
		targets := make([]string, 0, len(m.marked))
		for p := range m.marked {
			targets = append(targets, p)
		}
		sort.Strings(targets)
		return targets
	}
	if p := m.currentPath(); p != "" {
		return []string{p}
	}
	return nil
}

func (m model) commitTargets() []string {
	targets := m.selectedTargets()
	if len(targets) > 0 {
		return targets
	}
	return []string{"."}
}

func (m model) addTargets() []string {
	if len(m.marked) > 0 {
		targets := make([]string, 0, len(m.marked))
		for _, e := range m.entries {
			if e.Code == '?' {
				if _, ok := m.marked[e.Path]; ok {
					targets = append(targets, e.Path)
				}
			}
		}
		return targets
	}

	if len(m.entries) > 0 {
		entry := m.entries[m.cursor]
		if entry.Code == '?' {
			return []string{entry.Path}
		}
	}
	return nil
}

func (m model) maybeFollowDiff() tea.Cmd {
	// Changes 模式下光标移动即自动预览选中文件的 diff
	if m.mode != modeChanges {
		return nil
	}
	if p := m.currentPath(); p != "" {
		return diffCmd(p)
	}
	return nil
}

func (m model) scrollStart(rows int) int {
	return windowStart(m.cursor, len(m.entries), rows)
}

func windowStart(cursor, total, rows int) int {
	if rows <= 0 || total <= rows {
		return 0
	}
	start := cursor - rows/2
	if start < 0 {
		start = 0
	}
	maxStart := total - rows
	if start > maxStart {
		start = maxStart
	}
	return start
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func formatLogDate(t time.Time) string {
	if t.IsZero() {
		return "????-??-?? ??:??"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func fetchStatusCmd() tea.Cmd {
	return func() tea.Msg {
		out, err := runSVN("status")
		if err != nil {
			return statusMsg{raw: out, err: err}
		}
		return statusMsg{entries: parseStatus(out), raw: out}
	}
}

func diffCmd(path string) tea.Cmd {
	return func() tea.Msg {
		args := []string{"diff", "-x", "-U" + strconv.Itoa(diffContextLines), path}
		out, err := runSVN(args...)
		return previewMsg{
			title: "差异: " + path,
			body:  formatSVNResult(args, out, err),
		}
	}
}

func logCmd(path string) tea.Cmd {
	return func() tea.Msg {
		args := []string{"log", "-l", "20", path}
		out, err := runSVN(args...)
		return previewMsg{
			title: "SVN 日志: " + path,
			body:  formatSVNResult(args, out, err),
		}
	}
}

func fetchLogCmd() tea.Cmd {
	return func() tea.Msg {
		args := []string{"log", "--xml", "-v", "-l", "20", "."}
		out, err := runSVN(args...)
		if err != nil {
			return logListMsg{raw: out, err: err}
		}
		entries, parseErr := parseLogXML(out)
		if parseErr != nil {
			return logListMsg{raw: out, err: parseErr}
		}
		return logListMsg{entries: entries}
	}
}

func parseLogXML(out string) ([]logEntry, error) {
	var doc struct {
		Entries []struct {
			Revision string `xml:"revision,attr"`
			Author   string `xml:"author"`
			Date     string `xml:"date"`
			Message  string `xml:"msg"`
			Paths    []struct {
				Action string `xml:"action,attr"`
				Kind   string `xml:"kind,attr"`
				Path   string `xml:",chardata"`
			} `xml:"paths>path"`
		} `xml:"logentry"`
	}

	if err := xml.Unmarshal([]byte(out), &doc); err != nil {
		return nil, err
	}

	entries := make([]logEntry, 0, len(doc.Entries))
	for _, raw := range doc.Entries {
		entry := logEntry{
			Author:  strings.TrimSpace(raw.Author),
			Message: strings.TrimSpace(raw.Message),
		}
		if rev, err := strconv.ParseInt(strings.TrimSpace(raw.Revision), 10, 64); err == nil {
			entry.Revision = rev
		}
		if date := strings.TrimSpace(raw.Date); date != "" {
			if t, err := time.Parse(time.RFC3339, date); err == nil {
				entry.Date = t
			}
		}
		for _, p := range raw.Paths {
			entry.Paths = append(entry.Paths, changedPath{
				Action: strings.TrimSpace(p.Action),
				Kind:   strings.TrimSpace(p.Kind),
				Path:   strings.TrimSpace(p.Path),
			})
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func logFileDiffCmd(revision int64, path string) tea.Cmd {
	return func() tea.Msg {
		args := []string{"diff", "-c", strconv.FormatInt(revision, 10), "-x", "-U" + strconv.Itoa(diffContextLines), "^" + path}
		out, err := runSVN(args...)
		return logDiffMsg{
			revision: revision,
			path:     path,
			title:    fmt.Sprintf("差异: r%d %s", revision, path),
			body:     formatSVNResult(args, out, err),
		}
	}
}

func updateCmd() tea.Cmd {
	return func() tea.Msg {
		args := []string{"update"}
		out, err := runSVN(args...)
		return actionMsg{
			title:   "更新",
			body:    formatSVNResult(args, out, err),
			refresh: true,
		}
	}
}

func addCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		args := append([]string{"add", "--parents"}, paths...)
		out, err := runSVN(args...)
		return actionMsg{
			title:   "添加",
			body:    formatSVNResult(args, out, err),
			refresh: true,
		}
	}
}

func revertCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		args := append([]string{"revert"}, paths...)
		out, err := runSVN(args...)
		return actionMsg{
			title:   "还原",
			body:    formatSVNResult(args, out, err),
			refresh: true,
		}
	}
}

func commitCmd(paths []string, message string) tea.Cmd {
	return func() tea.Msg {
		args := []string{"commit", "-m", message}
		args = append(args, paths...)
		out, err := runSVN(args...)
		return actionMsg{
			title:   "提交",
			body:    formatSVNResult(args, out, err),
			refresh: true,
		}
	}
}

func parseStatus(out string) []statusEntry {
	var entries []statusEntry
	for _, raw := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " ")
		if line == "" {
			continue
		}
		if len(line) < 2 {
			continue
		}
		code := rune(line[0])
		path := ""
		if len(line) >= 9 {
			path = strings.TrimSpace(line[8:])
		}
		if path == "" {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			path = strings.Join(parts[1:], " ")
		}
		entries = append(entries, statusEntry{
			Code: code,
			Path: path,
			Raw:  line,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

func runSVN(args ...string) (string, error) {
	cmd := exec.Command("svn", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return strings.TrimRight(buf.String(), "\n"), err
}

func formatSVNResult(args []string, out string, err error) string {
	var b strings.Builder
	b.WriteString("$ svn ")
	b.WriteString(strings.Join(args, " "))
	b.WriteString("\n\n")
	if strings.TrimSpace(out) != "" {
		b.WriteString(out)
		b.WriteString("\n")
	}
	if err != nil {
		b.WriteString("\nerror: ")
		b.WriteString(err.Error())
		b.WriteString("\n")
	}
	if strings.TrimSpace(out) == "" && err == nil {
		b.WriteString("(no output)\n")
	}
	return strings.TrimSpace(b.String())
}

func statusCodeStyle(code rune) lipgloss.Style {
	switch code {
	case 'M':
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	case 'A':
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	case 'D':
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	case '?':
		return lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	case '!':
		return lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	case 'C':
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	}
}

func helpText() string {
	return strings.TrimSpace(`
LazySVN 按键：
  j/k 或方向键   移动
  g / G          顶部 / 底部
  空格 / 回车     标记 / 取消标记
  d              查看选中文件差异
  l              查看文件最近 20 条日志
  L 或 Tab       打开 SVN 日志 tab
  C              切回变化 tab
  列表顶部 ↑      聚焦 tab 栏，←/→ 选择
  ← / →          日志视图切换左右窗口
  +              切换差异显示方式
  日志内 Enter    查看文件差异
  日志内 Esc      关闭差异 / 退出日志
  c              提交标记文件（或当前）
  a              添加未版本化文件（?）
  v              还原标记/当前文件（需确认）
  u              svn update
  r              刷新状态
  h 或 ?          显示帮助
  q              退出
	`)
}

func cliHelp() string {
	return strings.TrimSpace(`
LazySVN

A LazyGit-style terminal UI for Subversion (SVN).

Usage:
  lazysvn
  lazysvn --version
  lazysvn update [tag]
  lazysvn uninstall
  lazysvn --help

Command:
  version      Show lazysvn version
  update       Self-update from hosted installer (defaults to latest)
  uninstall    Remove the current lazysvn binary
`)
}

func currentVersion() string {
	if v := normalizeVersion(version); v != "" && v != "dev" {
		return v
	}

	info, ok := debug.ReadBuildInfo()
	if ok && info != nil {
		if mv := normalizeVersion(info.Main.Version); mv != "" && mv != "(devel)" {
			return mv
		}
	}

	if gv := gitTagVersion(); gv != "" {
		return gv
	}

	return "0.0.0"
}

func normalizeVersion(v string) string {
	out := strings.TrimSpace(v)
	if out == "" || out == "(devel)" {
		return ""
	}
	if strings.HasPrefix(out, "refs/tags/") {
		out = strings.TrimPrefix(out, "refs/tags/")
	}
	if pseudoVersionRE.MatchString(out) {
		return ""
	}
	if strings.HasPrefix(out, "v") && len(out) > 1 {
		if r := rune(out[1]); r >= '0' && r <= '9' {
			out = out[1:]
		}
	}
	out = strings.SplitN(out, "-", 2)[0]
	out = strings.SplitN(out, "+", 2)[0]

	parts := strings.Split(out, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return ""
	}
	for _, p := range parts {
		if p == "" {
			return ""
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return ""
			}
		}
	}
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".")
}

func gitTagVersion() string {
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return ""
	}
	return normalizeVersion(out.String())
}

func updateHelp() string {
	return strings.TrimSpace(`
Usage:
  lazysvn update
  lazysvn update <tag>

Examples:
  lazysvn update
  lazysvn update v0.1.0

Env:
  INSTALL_DIR            Override install target directory
  LAZYSVN_INSTALL_URL    Override installer URL (default: https://lazysvn.sawirstudio.com/install.sh)
	`)
}

func uninstallHelp() string {
	return strings.TrimSpace(`
Usage:
  lazysvn uninstall

Notes:
  - Removes the currently running lazysvn binary.
  - For Homebrew installs, run: brew uninstall lazysvn
	`)
}

func runSelfUpdate(version string) error {
	installURL := strings.TrimSpace(os.Getenv("LAZYSVN_INSTALL_URL"))
	if installURL == "" {
		installURL = "https://lazysvn.sawirstudio.com/install.sh"
	}

	req, err := http.NewRequest(http.MethodGet, installURL, nil)
	if err != nil {
		return fmt.Errorf("create installer request: %w", err)
	}
	req.Header.Set("User-Agent", "lazysvn-self-update")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch installer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("installer request failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	script, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read installer: %w", err)
	}
	if len(script) == 0 {
		return fmt.Errorf("installer script is empty")
	}

	installDir := strings.TrimSpace(os.Getenv("INSTALL_DIR"))
	if installDir == "" {
		exePath, err := os.Executable()
		if err == nil {
			installDir = filepath.Dir(exePath)
		}
	}

	fmt.Printf("Updating lazysvn to %s...\n", version)
	cmd := exec.Command("sh")
	cmd.Stdin = bytes.NewReader(script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "VERSION="+version)
	if strings.TrimSpace(os.Getenv("INSTALL_DIR")) == "" && installDir != "" {
		cmd.Env = append(cmd.Env, "INSTALL_DIR="+installDir)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run installer: %w", err)
	}

	fmt.Println("Update complete.")
	return nil
}

func runSelfUninstall() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	resolvedPath := exePath
	if p, err := filepath.EvalSymlinks(exePath); err == nil && p != "" {
		resolvedPath = p
	}

	if strings.Contains(exePath, "/Cellar/") || strings.Contains(resolvedPath, "/Cellar/") {
		return fmt.Errorf("detected Homebrew-managed install (%s). Run: brew uninstall lazysvn", exePath)
	}

	if err := os.Remove(exePath); err != nil {
		return fmt.Errorf("remove %s: %w", exePath, err)
	}

	fmt.Printf("Removed %s\n", exePath)
	return nil
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	// 按显示宽度截断（中文/emoji 占两列），并保留 ANSI 转义序列
	return ansi.Truncate(s, maxLen, "…")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
