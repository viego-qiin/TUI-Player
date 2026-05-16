package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/qiinY/tui-player/internal/audio"
	"github.com/qiinY/tui-player/internal/player"
	"github.com/qiinY/tui-player/internal/playlist"
)

// 视图类型
type viewType int

const (
	viewMain viewType = iota
	viewAllSongs
	viewPlaylists
	viewPlaylistDetail
	viewCreatePlaylist
	viewDeletePlaylist
	viewAddToPlaylist
	viewRemoveFromPlaylist
	viewSelectDir
	viewAddSongsToPlaylist // 可视化多选添加歌曲
)

// 样式定义
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF6B6B")).
			PaddingTop(1).
			PaddingBottom(1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ECDC4"))

	nowPlayingStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFE66D"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#95E1D3"))

	menuItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F8F9FA"))

	menuKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF6B6B"))

	songItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E0E0E0"))

	songActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFE66D"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6C757D")).
			PaddingTop(1)

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF4757"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#2ED573"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#57606F"))

	separator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#373B40")).
			Render(strings.Repeat("─", 50))

	playingStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF6B6B"))
	formatStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6C757D")).
			Background(lipgloss.Color("#2D2F33")).
			Padding(0, 1)
	sizeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#495057"))

	progressEmpty = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#373B40")).
			Render("━")
	progressFill = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ECDC4")).
			Render("━")
)

// model 主模型
type model struct {
	view        viewType
	playlistMgr *playlist.Manager
	playerCtrl  *player.Controller
	audioPlayer *audio.Player
	musicDir    string

	// 列表相关
	cursor       int
	scrollOffset int
	maxVisible   int

	// 歌单相关
	selectedPlaylist string

	// 多选相关
	selected   map[int]bool // 选中的索引集合
	selectMode bool         // 是否进入多选模式（在添加视图时自动开启）

	// 输入相关
	textInput textinput.Model

	// 消息
	message      string
	messageIsErr bool
	msgDeadline  time.Time // 消息何时自动消失

	// 是否显示帮助
	showHelp bool

	// 窗口大小
	width  int
	height int
}

// New 创建 TUI 模型
func New(playlistMgr *playlist.Manager, playerCtrl *player.Controller, audioPlayer *audio.Player, musicDir string) *model {
	ti := textinput.New()
	ti.Placeholder = "输入名称..."
	ti.CharLimit = 50
	ti.Width = 30

	return &model{
		view:        viewMain,
		playlistMgr: playlistMgr,
		playerCtrl:  playerCtrl,
		audioPlayer: audioPlayer,
		musicDir:    musicDir,
		maxVisible:  15,
		textInput:   ti,
		selected:    make(map[int]bool),
	}
}

// Init 初始化
func (m *model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tickCmd())
}

// Update 处理消息
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.maxVisible = msg.Height - 8
		if m.maxVisible < 3 {
			m.maxVisible = 3
		}

	case tickMsg:
		if m.message != "" && !m.msgDeadline.IsZero() && time.Now().After(m.msgDeadline) {
			m.message = ""
		}
		return m, tickCmd()

	case tea.KeyMsg:
		// 输入模式下优先处理
		if m.view == viewCreatePlaylist || m.view == viewAddToPlaylist {
			return m.handleInputKey(msg)
		}

		// 全局快捷键
		switch {
		case msg.String() == "ctrl+c" || msg.String() == "q":
			return m, tea.Quit
		case msg.String() == "?":
			m.showHelp = !m.showHelp
			return m, nil
		case msg.String() == "esc":
			if m.view != viewMain {
				m.view = viewMain
				m.cursor = 0
				m.scrollOffset = 0
				m.message = ""
				m.selected = make(map[int]bool)
				return m, nil
			}
		}

		// 根据当前视图处理按键
		switch m.view {
		case viewMain:
			return m.handleMainViewKey(msg)
		case viewAllSongs:
			return m.handleAllSongsViewKey(msg)
		case viewPlaylists:
			return m.handlePlaylistsViewKey(msg)
		case viewPlaylistDetail:
			return m.handlePlaylistDetailViewKey(msg)
		case viewDeletePlaylist:
			return m.handleDeletePlaylistKey(msg)
		case viewRemoveFromPlaylist:
			return m.handleRemoveFromPlaylistKey(msg)
		case viewSelectDir:
			return m.handleSelectDirKey(msg)
		case viewAddSongsToPlaylist:
			return m.handleAddSongsToPlaylistKey(msg)
		}

	case scanCompleteMsg:
		if msg.count > 0 {
			m.playerCtrl.SetPlaylist(m.playlistMgr.GetAllSongs(), "全部音乐")
		}
		if msg.cached {
			m.setMessage(fmt.Sprintf("已加载 %d 首歌曲（缓存）", msg.count), false)
		} else {
			m.setMessage(fmt.Sprintf("扫描完成，共 %d 首歌曲", msg.count), false)
		}

	case scanDirCompleteMsg:
		m.musicDir = msg.dir
		m.playerCtrl.SetMusicDir(msg.dir)
		m.setMessage(fmt.Sprintf("已切换到目录: %s", msg.dir), false)
		// 自动扫描新目录
		return m, m.scanMusicDir()

	case errMsg:
		m.setMessage(msg.err.Error(), true)
	}

	return m, nil
}

// View 渲染界面
func (m *model) View() string {
	if m.showHelp {
		return m.helpView()
	}

	switch m.view {
	case viewMain:
		return m.mainView()
	case viewAllSongs:
		return m.allSongsView()
	case viewPlaylists:
		return m.playlistsView()
	case viewPlaylistDetail:
		return m.playlistDetailView()
	case viewCreatePlaylist:
		return m.createPlaylistView()
	case viewDeletePlaylist:
		return m.deletePlaylistView()
	case viewAddToPlaylist:
		return m.addToPlaylistView()
	case viewRemoveFromPlaylist:
		return m.removeFromPlaylistView()
	case viewSelectDir:
		return m.selectDirView()
	case viewAddSongsToPlaylist:
		return m.addSongsToPlaylistView()
	default:
		return m.mainView()
	}
}

// ─── 主视图 ───

func (m *model) mainView() string {
	var b strings.Builder

	status := m.playerCtrl.GetStatus()

	// 标题行 + 当前播放
	b.WriteString(titleStyle.Render("🎵 TUI-Player"))
	if status.CurrentSong != nil {
		b.WriteString("  ")
		if status.IsPaused {
			b.WriteString(nowPlayingStyle.Render("⏸"))
		} else {
			b.WriteString(nowPlayingStyle.Render("♪"))
		}
		b.WriteString(nowPlayingStyle.Render(" " + status.CurrentSong.Title))
	}
	b.WriteString("\n")

	// 第二行：状态信息 + 进度条
	if status.CurrentSong != nil {
		playState := "▶"
		if status.IsPaused {
			playState = "⏸"
		}
		b.WriteString(infoStyle.Render(fmt.Sprintf("   %s %s | %s | %d/%d",
			playState, player.PlayModeName(status.PlayMode), status.SourceName, status.CurrentIndex+1, status.TotalSongs)))
		b.WriteString("\n")
		b.WriteString(m.progressBar())
	} else {
		if len(m.playlistMgr.GetAllSongs()) == 0 {
			b.WriteString(dimStyle.Render("   暂无音乐，按 f 选择文件夹扫描"))
		} else {
			b.WriteString(dimStyle.Render("   按 space 开始播放"))
		}
	}
	b.WriteString("\n")
	b.WriteString(separator)
	b.WriteString("\n")

	// 菜单（两列，左右分别对齐）
	leftItems := []struct{ key, desc string }{
		{"1", "全部音乐"},
		{"f", "切换目录/重扫"},
		{"n/b", "上下曲"},
		{"m", modeToggleText(status.PlayMode)},
	}
	rightItems := []struct{ key, desc string }{
		{"2", "歌单(创建/删除)"},
		{"space", "播放/暂停"},
		{"t", "停止"},
	}
	maxRows := len(leftItems)
	if len(rightItems) > maxRows {
		maxRows = len(rightItems)
	}
	colW := 30 // 左列固定显示宽度
	for row := 0; row < maxRows; row++ {
		var left, right string
		if row < len(leftItems) {
			l := leftItems[row]
			left = fmt.Sprintf("  %s %s",
				menuKeyStyle.Render("["+l.key+"]"),
				menuItemStyle.Render(l.desc))
		}
		if row < len(rightItems) {
			r := rightItems[row]
			right = fmt.Sprintf("%s %s",
				menuKeyStyle.Render("["+r.key+"]"),
				menuItemStyle.Render(r.desc))
		}
		// 用 lipgloss 计算可视宽度来对齐
		lw := lipgloss.Width(left)
		pad := colW - lw
		if pad < 1 {
			pad = 1
		}
		b.WriteString(left + strings.Repeat(" ", pad) + right)
		b.WriteString("\n")
	}

	// 消息
	if m.message != "" {
		if m.messageIsErr {
			b.WriteString(errorStyle.Render("❌ " + m.message))
		} else {
			b.WriteString(successStyle.Render("✅ " + m.message))
		}
		b.WriteString("\n")
	}

	// 底部状态栏
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf(" q:退出  ?:帮助  esc:返回  %s", m.musicDir)))

	return b.String()
}

// ─── 全部音乐视图 ───

func (m *model) allSongsView() string {
	var b strings.Builder

	songs := m.playlistMgr.GetAllSongs()
	b.WriteString(titleStyle.Render(fmt.Sprintf("📀 全部音乐 (%d 首)", len(songs))))
	b.WriteString(helpStyle.Render("  enter:播放  space:选中  ctrl+a:全选  PgUp/Dn:翻页"))
	b.WriteString("\n\n")

	if len(songs) == 0 {
		b.WriteString(dimStyle.Render("   暂无音乐，请先扫描文件夹"))
		return b.String()
	}

	m.ensureCursorVisible(len(songs))
	end := m.scrollOffset + m.maxVisible
	if end > len(songs) {
		end = len(songs)
	}

	for i := m.scrollOffset; i < end; i++ {
		b.WriteString(m.songLine(i, songs[i]))
		b.WriteString("\n")
	}

	return b.String()
}

// ─── 歌单列表视图 ───

func (m *model) playlistsView() string {
	var b strings.Builder

	playlists := m.playlistMgr.GetPlaylists()
	b.WriteString(titleStyle.Render(fmt.Sprintf("📂 歌单列表 (%d 个)", len(playlists))))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("enter:查看  c:创建  d:删除  esc:返回  PgUp/Dn:翻页"))
	b.WriteString("\n\n")

	if len(playlists) == 0 {
		b.WriteString(dimStyle.Render("   暂无歌单，按 c 创建"))
		return b.String()
	}

	m.ensureCursorVisible(len(playlists))

	end := m.scrollOffset + m.maxVisible
	if end > len(playlists) {
		end = len(playlists)
	}

	for i := m.scrollOffset; i < end; i++ {
		songs, _ := m.playlistMgr.GetPlaylistSongs(playlists[i])
		prefix := "  "
		style := songItemStyle
		if i == m.cursor {
			prefix = "▸ "
			style = songActiveStyle
		}
		b.WriteString(style.Render(fmt.Sprintf("%s%2d. %s (%d 首)", prefix, i+1, playlists[i], len(songs))))
		b.WriteString("\n")
	}

	return b.String()
}

// ─── 歌单详情视图 ───

func (m *model) playlistDetailView() string {
	var b strings.Builder

	songs, err := m.playlistMgr.GetPlaylistSongs(m.selectedPlaylist)
	if err != nil {
		return errorStyle.Render(err.Error())
	}

	b.WriteString(titleStyle.Render(fmt.Sprintf("📂 %s (%d 首)", m.selectedPlaylist, len(songs))))
	b.WriteString(helpStyle.Render("  enter:播放  a:添加  r:移除  space:选中  PgUp/Dn:翻页"))
	b.WriteString("\n\n")

	if len(songs) == 0 {
		b.WriteString(dimStyle.Render("   歌单为空，按 a 添加歌曲"))
		return b.String()
	}

	m.ensureCursorVisible(len(songs))
	end := m.scrollOffset + m.maxVisible
	if end > len(songs) {
		end = len(songs)
	}

	for i := m.scrollOffset; i < end; i++ {
		b.WriteString(m.songLine(i, songs[i]))
		b.WriteString("\n")
	}

	return b.String()
}

// ─── 创建歌单视图 ───

func (m *model) createPlaylistView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("📝 创建新歌单"))
	b.WriteString("\n\n")
	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("enter:确认  esc:取消"))

	return b.String()
}

// ─── 删除歌单视图 ───

func (m *model) deletePlaylistView() string {
	var b strings.Builder

	playlists := m.playlistMgr.GetPlaylists()
	b.WriteString(titleStyle.Render("🗑 选择要删除的歌单"))
	b.WriteString("\n\n")

	if len(playlists) == 0 {
		b.WriteString(dimStyle.Render("   暂无歌单"))
		return b.String()
	}

	m.ensureCursorVisible(len(playlists))

	end := m.scrollOffset + m.maxVisible
	if end > len(playlists) {
		end = len(playlists)
	}

	for i := m.scrollOffset; i < end; i++ {
		prefix := "  "
		style := songItemStyle
		if i == m.cursor {
			prefix = "▸ "
			style = songActiveStyle
		}
		b.WriteString(style.Render(fmt.Sprintf("%s%2d. %s", prefix, i+1, playlists[i])))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("enter:确认删除  esc:取消"))

	return b.String()
}

// ─── 添加到歌单视图 ───

func (m *model) addToPlaylistView() string {
	var b strings.Builder

	allSongs := m.playlistMgr.GetAllSongs()

	b.WriteString(titleStyle.Render("📥 添加歌曲到歌单"))
	b.WriteString("\n\n")
	b.WriteString(infoStyle.Render("格式: 歌单名,歌曲编号1,歌曲编号2,..."))
	b.WriteString("\n\n")
	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")

	// 显示可选歌曲
	b.WriteString(dimStyle.Render("可选歌曲:"))
	b.WriteString("\n")
	for i, song := range allSongs {
		b.WriteString(fmt.Sprintf("  %2d. %s\n", i+1, song.Title))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("例如: 我的歌单,1,3,5"))

	return b.String()
}

// ─── 从歌单移除视图 ───

func (m *model) removeFromPlaylistView() string {
	var b strings.Builder

	songs, err := m.playlistMgr.GetPlaylistSongs(m.selectedPlaylist)
	if err != nil {
		return errorStyle.Render(err.Error())
	}

	b.WriteString(titleStyle.Render(fmt.Sprintf("🗑 从 %s 移除歌曲", m.selectedPlaylist)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("enter:移除  esc:取消  ↑/↓:选择"))
	b.WriteString("\n\n")

	if len(songs) == 0 {
		b.WriteString(dimStyle.Render("   歌单为空"))
		return b.String()
	}

	m.ensureCursorVisible(len(songs))

	end := m.scrollOffset + m.maxVisible
	if end > len(songs) {
		end = len(songs)
	}

	for i := m.scrollOffset; i < end; i++ {
		prefix := "  "
		style := songItemStyle
		if i == m.cursor {
			prefix = "▸ "
			style = songActiveStyle
		}
		b.WriteString(style.Render(fmt.Sprintf("%s%2d. %s", prefix, i+1, songs[i].Title)))
		b.WriteString("\n")
	}

	return b.String()
}

// ─── 帮助视图 ───

func (m *model) helpView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("⌨ 快捷键帮助"))
	b.WriteString("\n\n")

	helpItems := []struct {
		key  string
		desc string
	}{
		{"↑/k, ↓/j", "上下移动光标"},
		{"PgUp/Dn", "翻页"},
		{"enter", "确认 / 播放选中歌曲"},
		{"space", "选中/取消歌曲 / 播放暂停"},
		{"ctrl+a", "全选/取消全选"},
		{"n", "下一曲"},
		{"b", "上一曲"},
		{"t", "停止播放"},
		{"m", "切换顺序/随机播放模式"},
		{"1", "查看全部音乐"},
		{"2", "查看歌单列表（c:创建 d:删除）"},
		{"a", "添加歌曲到歌单（可视化多选）"},
		{"r", "从歌单移除歌曲"},
		{"f", "选择/扫描音乐文件夹（留空=重扫）"},
		{"esc", "返回上一级"},
		{"?", "显示/隐藏帮助"},
		{"q", "退出程序"},
	}

	for _, item := range helpItems {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			menuKeyStyle.Render(fmt.Sprintf("%-12s", item.key)),
			menuItemStyle.Render(item.desc),
		))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("按 ? 返回"))

	return b.String()
}

// ─── 键盘处理 ───

func (m *model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "enter":
		return m.handleInputSubmit()
	case msg.String() == "esc":
		m.view = viewMain
		m.textInput.Blur()
		m.textInput.SetValue("")
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m *model) handleMainViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "1":
		m.view = viewAllSongs
		m.cursor = 0
		m.scrollOffset = 0
	case "2":
		m.view = viewPlaylists
		m.cursor = 0
		m.scrollOffset = 0
	case "f":
		m.view = viewSelectDir
		m.cursor = 0
		m.scrollOffset = 0
		m.textInput.Placeholder = "输入文件夹路径（留空按 enter 重新扫描）..."
		m.textInput.Focus()
		m.textInput.SetValue("")
	case " ", "p":
		m.handlePlayPause()
	case "n":
		if err := m.playerCtrl.Next(); err != nil {
			m.setMessage(err.Error(), true)
		}
	case "b":
		if err := m.playerCtrl.Prev(); err != nil {
			m.setMessage(err.Error(), true)
		}
	case "t":
		m.playerCtrl.Stop()
		m.setMessage("播放已停止", false)
	case "m":
		m.togglePlayMode()
	}
	return m, nil
}

func (m *model) handleAllSongsViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	songs := m.playlistMgr.GetAllSongs()
	if len(songs) == 0 {
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(songs)-1 {
			m.cursor++
		}
	case "pgup", "ctrl+u":
		m.cursor -= m.maxVisible
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "pgdown", "ctrl+d":
		m.cursor += m.maxVisible
		if m.cursor >= len(songs) {
			m.cursor = len(songs) - 1
		}
	case " ":
		// 空格切换选中状态
		m.selected[m.cursor] = !m.selected[m.cursor]
	case "ctrl+a":
		// 全选/取消全选
		allSelected := true
		for i := 0; i < len(songs); i++ {
			if !m.selected[i] {
				allSelected = false
				break
			}
		}
		for i := 0; i < len(songs); i++ {
			m.selected[i] = !allSelected
		}
	case "enter":
		m.playerCtrl.SetPlaylist(songs, "全部音乐")
		if err := m.playerCtrl.PlayIndex(m.cursor); err != nil {
			m.setMessage(err.Error(), true)
		}
		m.view = viewMain
	}
	return m, nil
}

func (m *model) handlePlaylistsViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	playlists := m.playlistMgr.GetPlaylists()

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(playlists)-1 {
			m.cursor++
		}
	case "pgup", "ctrl+u":
		m.cursor -= m.maxVisible
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "pgdown", "ctrl+d":
		m.cursor += m.maxVisible
		if m.cursor >= len(playlists) {
			m.cursor = len(playlists) - 1
		}
	case "enter":
		if len(playlists) > 0 && m.cursor < len(playlists) {
			m.selectedPlaylist = playlists[m.cursor]
			m.view = viewPlaylistDetail
			m.cursor = 0
			m.scrollOffset = 0
		}
	case "c":
		m.view = viewCreatePlaylist
		m.textInput.Placeholder = "输入歌单名..."
		m.textInput.Focus()
		m.textInput.SetValue("")
	case "d":
		m.view = viewDeletePlaylist
		m.cursor = 0
		m.scrollOffset = 0
	}
	return m, nil
}

func (m *model) handlePlaylistDetailViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	songs, _ := m.playlistMgr.GetPlaylistSongs(m.selectedPlaylist)

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(songs)-1 {
			m.cursor++
		}
	case "pgup", "ctrl+u":
		m.cursor -= m.maxVisible
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "pgdown", "ctrl+d":
		m.cursor += m.maxVisible
		if m.cursor >= len(songs) {
			m.cursor = len(songs) - 1
		}
	case " ":
		m.selected[m.cursor] = !m.selected[m.cursor]
	case "ctrl+a":
		allSelected := true
		for i := 0; i < len(songs); i++ {
			if !m.selected[i] {
				allSelected = false
				break
			}
		}
		for i := 0; i < len(songs); i++ {
			m.selected[i] = !allSelected
		}
	case "enter":
		if len(songs) > 0 && m.cursor < len(songs) {
			m.playerCtrl.SetPlaylist(songs, m.selectedPlaylist)
			if err := m.playerCtrl.PlayIndex(m.cursor); err != nil {
				m.setMessage(err.Error(), true)
			}
			m.view = viewMain
		}
	case "a":
		// 打开可视化多选添加界面
		allSongs := m.playlistMgr.GetAllSongs()
		if len(allSongs) == 0 {
			m.setMessage("暂无音乐可选", true)
			return m, nil
		}
		m.view = viewAddSongsToPlaylist
		m.cursor = 0
		m.scrollOffset = 0
		m.selected = make(map[int]bool)
	case "r":
		if len(songs) > 0 {
			m.view = viewRemoveFromPlaylist
			m.cursor = 0
			m.scrollOffset = 0
		}
	}
	return m, nil
}

func (m *model) handleDeletePlaylistKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	playlists := m.playlistMgr.GetPlaylists()

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(playlists)-1 {
			m.cursor++
		}
	case "pgup", "ctrl+u":
		m.cursor -= m.maxVisible
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "pgdown", "ctrl+d":
		m.cursor += m.maxVisible
		if m.cursor >= len(playlists) {
			m.cursor = len(playlists) - 1
		}
	case "enter":
		if len(playlists) > 0 && m.cursor < len(playlists) {
			name := playlists[m.cursor]
			if err := m.playlistMgr.DeletePlaylist(name); err != nil {
				m.setMessage(err.Error(), true)
			} else {
				m.setMessage(fmt.Sprintf("歌单 '%s' 已删除", name), false)
			}
			m.view = viewMain
			m.cursor = 0
		}
	}
	return m, nil
}

func (m *model) handleRemoveFromPlaylistKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	songs, _ := m.playlistMgr.GetPlaylistSongs(m.selectedPlaylist)

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(songs)-1 {
			m.cursor++
		}
	case "pgup", "ctrl+u":
		m.cursor -= m.maxVisible
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "pgdown", "ctrl+d":
		m.cursor += m.maxVisible
		if m.cursor >= len(songs) {
			m.cursor = len(songs) - 1
		}
	case "enter":
		if len(songs) > 0 && m.cursor < len(songs) {
			if err := m.playlistMgr.RemoveSongFromPlaylist(m.selectedPlaylist, m.cursor); err != nil {
				m.setMessage(err.Error(), true)
			} else {
				m.setMessage("歌曲已从歌单移除", false)
			}
			m.view = viewPlaylistDetail
			m.cursor = 0
		}
	}
	return m, nil
}

// ─── 输入提交处理 ───

func (m *model) handleInputSubmit() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.textInput.Value())
	if value == "" {
		m.setMessage("输入不能为空", true)
		return m, nil
	}

	switch m.view {
	case viewCreatePlaylist:
		if err := m.playlistMgr.CreatePlaylist(value); err != nil {
			m.setMessage(err.Error(), true)
		} else {
			m.setMessage(fmt.Sprintf("歌单 '%s' 创建成功", value), false)
		}
		m.view = viewMain

	case viewAddToPlaylist:
		parts := strings.Split(value, ",")
		if len(parts) < 2 {
			m.setMessage("格式错误，请使用: 歌单名,编号1,编号2,...", true)
			return m, nil
		}
		playlistName := strings.TrimSpace(parts[0])
		allSongs := m.playlistMgr.GetAllSongs()
		added := 0

		for _, part := range parts[1:] {
			idx, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || idx < 1 || idx > len(allSongs) {
				continue
			}
			if err := m.playlistMgr.AddSongToPlaylist(playlistName, allSongs[idx-1]); err == nil {
				added++
			}
		}

		if added > 0 {
			m.setMessage(fmt.Sprintf("成功添加 %d 首歌曲到 '%s'", added, playlistName), false)
		} else {
			m.setMessage("未添加任何歌曲", true)
		}
		m.view = viewMain
	}

	m.textInput.Blur()
	m.textInput.SetValue("")
	return m, nil
}

// ─── 辅助方法 ───

func (m *model) isCurrentSong(path string) bool {
	status := m.playerCtrl.GetStatus()
	return status.CurrentSong != nil && status.CurrentSong.FilePath == path
}

func formatSize(size int64) string {
	switch {
	case size > 1024*1024*1024:
		return fmt.Sprintf("%.1fG", float64(size)/1024/1024/1024)
	case size > 1024*1024:
		return fmt.Sprintf("%.1fM", float64(size)/1024/1024)
	case size > 1024:
		return fmt.Sprintf("%.1fK", float64(size)/1024)
	default:
		return fmt.Sprintf("%dB", size)
	}
}

func fileExtStyle(ext string) string {
	return formatStyle.Render(strings.ToUpper(ext[1:]))
}

// songLine 渲染歌曲行（带高亮、选中、格式标签、大小）
func (m *model) songLine(i int, song playlist.Song) string {
	playing := m.isCurrentSong(song.FilePath)
	sel := m.selected[i]

	cursorMark := "  "
	songSty := songItemStyle

	if i == m.cursor {
		cursorMark = "▸ "
		songSty = songActiveStyle
	}
	if playing {
		cursorMark = "♪ "
		songSty = playingStyle
	}
	if playing && i == m.cursor {
		cursorMark = "♪▸"
	}

	selMark := " "
	if sel {
		selMark = "✓"
	}

	main := fmt.Sprintf("%s%s%3d. %s", cursorMark, selMark, i+1, song.Title)
	tag := fileExtStyle(filepath.Ext(song.FilePath))
	sz := sizeStyle.Render(" " + formatSize(song.Size))
	right := tag + sz

	styled := songSty.Render(main)
	visualW := lipgloss.Width(styled)
	rightW := lipgloss.Width(right)
	pad := 60 - visualW - rightW
	if pad < 2 {
		pad = 2
	}
	return styled + strings.Repeat(" ", pad) + right
}

func (m *model) ensureCursorVisible(total int) {
	if total == 0 {
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= total {
		m.cursor = total - 1
	}
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+m.maxVisible {
		m.scrollOffset = m.cursor - m.maxVisible + 1
	}
}

func (m *model) handlePlayPause() {
	m.message = ""
	status := m.playerCtrl.GetStatus()
	if status.CurrentSong == nil {
		songs := m.playlistMgr.GetAllSongs()
		if len(songs) == 0 {
			m.setMessage("暂无音乐，请先扫描文件夹", true)
			return
		}
		m.playerCtrl.SetPlaylist(songs, "全部音乐")
		if err := m.playerCtrl.Play(); err != nil {
			m.setMessage(err.Error(), true)
		}
	} else if status.IsPaused {
		m.playerCtrl.Resume()
	} else if status.IsPlaying {
		m.playerCtrl.Pause()
	} else {
		// 有歌曲但暂停/未播放：从当前位置开始
		if status.CurrentIndex >= 0 && status.CurrentIndex < status.TotalSongs {
			if err := m.playerCtrl.PlayIndex(status.CurrentIndex); err != nil {
				m.setMessage(err.Error(), true)
			}
		}
	}
}

func (m *model) togglePlayMode() {
	currentMode := m.playerCtrl.GetPlayMode()
	if currentMode == player.PlayModeOrder {
		m.playerCtrl.SetPlayMode(player.PlayModeShuffle)
		m.setMessage("已切换为随机播放模式", false)
	} else {
		m.playerCtrl.SetPlayMode(player.PlayModeOrder)
		m.setMessage("已切换为顺序播放模式", false)
	}
}

func modeToggleText(mode player.PlayMode) string {
	if mode == player.PlayModeOrder {
		return "切换随机播放"
	}
	return "切换顺序播放"
}

func (m *model) scanMusicDir() tea.Cmd {
	return func() tea.Msg {
		songs, cached, err := m.playlistMgr.ScanMusicDirCached(m.musicDir)
		if err != nil {
			return errMsg{err: err}
		}
		return scanCompleteMsg{count: len(songs), cached: cached}
	}
}

func (m *model) setMessage(msg string, isErr bool) {
	m.message = msg
	m.messageIsErr = isErr
	if msg != "" {
		m.msgDeadline = time.Now().Add(5 * time.Second)
	}
}

// errMsg 错误消息
type errMsg struct {
	err error
}

func (e errMsg) Error() string { return e.err.Error() }

// scanCompleteMsg 扫描完成消息
type scanCompleteMsg struct {
	count  int
	cached bool
}

// scanDirCompleteMsg 目录切换完成消息
type scanDirCompleteMsg struct {
	dir string
}

// ─── 选择文件夹视图 ───

func (m *model) selectDirView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("📁 选择/扫描音乐文件夹"))
	b.WriteString("\n\n")
	b.WriteString(infoStyle.Render("当前目录: "))
	b.WriteString(m.musicDir)
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("输入新路径切换目录，直接按 enter 重新扫描当前目录:"))
	b.WriteString("\n\n")
	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("enter:确认  esc:取消"))

	return b.String()
}

func (m *model) handleSelectDirKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "enter":
		dir := strings.TrimSpace(m.textInput.Value())
		m.textInput.Blur()
		m.textInput.SetValue("")
		m.view = viewMain
		if dir == "" {
			// 空输入 = 重新扫描当前目录
			return m, m.scanMusicDir()
		}
		// 检查目录是否存在
		info, err := os.Stat(dir)
		if err != nil {
			m.setMessage(fmt.Sprintf("无法访问目录: %v", err), true)
			return m, nil
		}
		if !info.IsDir() {
			m.setMessage("路径不是有效的目录", true)
			return m, nil
		}
		return m, func() tea.Msg {
			return scanDirCompleteMsg{dir: dir}
		}
	case msg.String() == "esc":
		m.view = viewMain
		m.textInput.Blur()
		m.textInput.SetValue("")
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

// ─── 进度条 ───

func (m *model) progressBar() string {
	elapsed := m.audioPlayer.Elapsed()
	total := m.audioPlayer.TotalDuration()

	var pct float64
	if total > 0 {
		pct = float64(elapsed) / float64(total)
		if pct > 1 {
			pct = 1
		}
	}

	barWidth := 30
	filled := int(pct * float64(barWidth))
	bar := strings.Repeat(progressFill, filled) + strings.Repeat(progressEmpty, barWidth-filled)

	elapsedStr := formatDuration(elapsed)
	totalStr := formatDuration(total)
	if total <= 0 {
		totalStr = "--:--"
	}

	return fmt.Sprintf("%s %s/%s", bar, elapsedStr, totalStr)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "00:00"
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*50, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

// ─── 可视化多选添加歌曲到歌单视图 ───

func (m *model) addSongsToPlaylistView() string {
	var b strings.Builder

	allSongs := m.playlistMgr.GetAllSongs()
	selectedCount := 0
	for i := range allSongs {
		if m.selected[i] {
			selectedCount++
		}
	}

	b.WriteString(titleStyle.Render(fmt.Sprintf("📥 添加歌曲到 %s", m.selectedPlaylist)))
	b.WriteString(statusStyle.Render(fmt.Sprintf("  [已选 %d 首]", selectedCount)))
	b.WriteString(helpStyle.Render("  space:选中  ctrl+a:全选  enter:确认  esc:取消"))
	b.WriteString("\n\n")

	if len(allSongs) == 0 {
		b.WriteString(dimStyle.Render("   暂无音乐可选"))
		return b.String()
	}

	m.ensureCursorVisible(len(allSongs))
	end := m.scrollOffset + m.maxVisible
	if end > len(allSongs) {
		end = len(allSongs)
	}

	for i := m.scrollOffset; i < end; i++ {
		b.WriteString(m.songLine(i, allSongs[i]))
		b.WriteString("\n")
	}

	return b.String()
}

func (m *model) handleAddSongsToPlaylistKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	allSongs := m.playlistMgr.GetAllSongs()

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(allSongs)-1 {
			m.cursor++
		}
	case "pgup", "ctrl+u":
		m.cursor -= m.maxVisible
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "pgdown", "ctrl+d":
		m.cursor += m.maxVisible
		if m.cursor >= len(allSongs) {
			m.cursor = len(allSongs) - 1
		}
	case " ":
		m.selected[m.cursor] = !m.selected[m.cursor]
	case "ctrl+a":
		allSelected := true
		for i := 0; i < len(allSongs); i++ {
			if !m.selected[i] {
				allSelected = false
				break
			}
		}
		for i := 0; i < len(allSongs); i++ {
			m.selected[i] = !allSelected
		}
	case "enter":
		added := 0
		for i, selected := range m.selected {
			if selected {
				if err := m.playlistMgr.AddSongToPlaylist(m.selectedPlaylist, allSongs[i]); err == nil {
					added++
				}
			}
		}
		if added > 0 {
			m.setMessage(fmt.Sprintf("成功添加 %d 首歌曲到 '%s'", added, m.selectedPlaylist), false)
		} else {
			m.setMessage("未选中任何歌曲", true)
		}
		m.selected = make(map[int]bool)
		m.view = viewMain
	}
	return m, nil
}

// Run 启动 TUI
func Run(playlistMgr *playlist.Manager, playerCtrl *player.Controller, audioPlayer *audio.Player, musicDir string) error {
	mdl := New(playlistMgr, playerCtrl, audioPlayer, musicDir)
	p := tea.NewProgram(mdl, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
