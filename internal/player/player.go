package player

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"

	"github.com/qiinY/tui-player/internal/audio"
	"github.com/qiinY/tui-player/internal/playlist"
)

// PlayMode 播放模式
type PlayMode int

const (
	PlayModeOrder   PlayMode = iota // 顺序播放
	PlayModeShuffle                 // 随机播放
)

// PlayModeName 返回播放模式名称
func PlayModeName(mode PlayMode) string {
	switch mode {
	case PlayModeOrder:
		return "顺序播放"
	case PlayModeShuffle:
		return "随机播放"
	default:
		return "未知"
	}
}

// Status 播放器状态
type Status struct {
	CurrentSong  *playlist.Song
	IsPlaying    bool
	IsPaused     bool
	PlayMode     PlayMode
	CurrentIndex int
	TotalSongs   int
	SourceName   string // 当前播放源名称（"全部音乐" 或歌单名）
}

// Controller 播放器控制器
type Controller struct {
	mu           sync.Mutex
	audioPlayer  *audio.Player
	playlistMgr  *playlist.Manager
	songs        []playlist.Song
	currentIndex int
	playMode     PlayMode
	sourceName   string
	shuffleOrder []int
	musicDir     string // 当前音乐目录（TUI 中切换后更新）
}

// NewController 创建播放器控制器
func NewController(audioPlayer *audio.Player, playlistMgr *playlist.Manager) *Controller {
	c := &Controller{
		audioPlayer:  audioPlayer,
		playlistMgr:  playlistMgr,
		currentIndex: -1,
		playMode:     PlayModeOrder,
	}
	audioPlayer.SetOnFinished(c.onSongFinished)
	return c
}

// SetPlaylist 设置当前播放列表
func (c *Controller) SetPlaylist(songs []playlist.Song, sourceName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.songs = songs
	c.sourceName = sourceName
	c.currentIndex = -1
	c.generateShuffleOrder()
}

// SetPlayMode 设置播放模式
func (c *Controller) SetPlayMode(mode PlayMode) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.playMode = mode
	c.generateShuffleOrder()
}

// GetPlayMode 获取当前播放模式
func (c *Controller) GetPlayMode() PlayMode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.playMode
}

// generateShuffleOrder 生成随机播放顺序
func (c *Controller) generateShuffleOrder() {
	count := len(c.songs)
	if count == 0 {
		c.shuffleOrder = nil
		return
	}
	c.shuffleOrder = rand.Perm(count)
}

// SetCurrentIndex 仅设置当前索引位置，不播放（用于恢复状态）
func (c *Controller) SetCurrentIndex(index int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index >= 0 && index < len(c.songs) {
		c.currentIndex = index
	}
}

// PlayIndex 播放指定索引的歌曲
func (c *Controller) PlayIndex(index int) error {
	c.mu.Lock()
	songs := c.songs
	c.mu.Unlock()

	if index < 0 || index >= len(songs) {
		return fmt.Errorf("索引 %d 超出范围（0-%d）", index, len(songs)-1)
	}

	song := songs[index]

	if err := c.audioPlayer.Play(song.FilePath); err != nil {
		return fmt.Errorf("播放失败: %w", err)
	}

	c.mu.Lock()
	c.currentIndex = index
	c.mu.Unlock()

	return nil
}

// Play 开始播放
func (c *Controller) Play() error {
	c.mu.Lock()
	songs := c.songs
	playMode := c.playMode
	c.mu.Unlock()

	if len(songs) == 0 {
		return fmt.Errorf("播放列表为空")
	}

	var index int
	if playMode == PlayModeShuffle {
		c.mu.Lock()
		if len(c.shuffleOrder) > 0 {
			index = c.shuffleOrder[0]
		}
		c.mu.Unlock()
	} else {
		index = 0
	}

	return c.PlayIndex(index)
}

// Pause 暂停
func (c *Controller) Pause() {
	c.audioPlayer.Pause()
}

// Resume 恢复
func (c *Controller) Resume() {
	c.audioPlayer.Resume()
}

// Stop 停止
func (c *Controller) Stop() {
	c.audioPlayer.Stop()
}

// Next 下一曲（跳过无法播放的文件）
func (c *Controller) Next() error {
	c.mu.Lock()
	songs := c.songs
	playMode := c.playMode
	currentIdx := c.currentIndex
	c.mu.Unlock()

	if len(songs) == 0 {
		return fmt.Errorf("播放列表为空")
	}

	startIdx := currentIdx
	for {
		var nextIndex int
		if playMode == PlayModeShuffle {
			c.mu.Lock()
			order := c.shuffleOrder
			pos := -1
			for i, idx := range order {
				if idx == currentIdx {
					pos = i
					break
				}
			}
			if pos >= 0 && pos+1 < len(order) {
				nextIndex = order[pos+1]
			} else {
				nextIndex = order[0]
			}
			c.mu.Unlock()
		} else {
			nextIndex = (currentIdx + 1) % len(songs)
		}

		if nextIndex == startIdx {
			return fmt.Errorf("无可播放的歌曲")
		}

		if err := c.PlayIndex(nextIndex); err == nil {
			return nil
		}
		currentIdx = nextIndex
	}
}

// Prev 上一曲（跳过无法播放的文件）
func (c *Controller) Prev() error {
	c.mu.Lock()
	songs := c.songs
	playMode := c.playMode
	currentIdx := c.currentIndex
	c.mu.Unlock()

	if len(songs) == 0 {
		return fmt.Errorf("播放列表为空")
	}

	startIdx := currentIdx
	for {
		var prevIndex int
		if playMode == PlayModeShuffle {
			c.mu.Lock()
			order := c.shuffleOrder
			pos := -1
			for i, idx := range order {
				if idx == currentIdx {
					pos = i
					break
				}
			}
			if pos > 0 {
				prevIndex = order[pos-1]
			} else {
				prevIndex = order[len(order)-1]
			}
			c.mu.Unlock()
		} else {
			prevIndex = (currentIdx - 1 + len(songs)) % len(songs)
		}

		if prevIndex == startIdx {
			return fmt.Errorf("无可播放的歌曲")
		}

		if err := c.PlayIndex(prevIndex); err == nil {
			return nil
		}
		currentIdx = prevIndex
	}
}

// onSongFinished 歌曲播放完成回调
func (c *Controller) onSongFinished() {
	c.mu.Lock()
	songs := c.songs
	playMode := c.playMode
	currentIdx := c.currentIndex
	c.mu.Unlock()

	if len(songs) == 0 {
		return
	}

	// 尝试播放下一个可用的歌曲（跳过无法播放的文件）
	startIdx := currentIdx
	for {
		var nextIndex int
		if playMode == PlayModeShuffle {
			c.mu.Lock()
			order := c.shuffleOrder
			pos := -1
			for i, idx := range order {
				if idx == currentIdx {
					pos = i
					break
				}
			}
			if pos >= 0 && pos+1 < len(order) {
				nextIndex = order[pos+1]
			} else {
				nextIndex = order[0]
			}
			c.mu.Unlock()
		} else {
			nextIndex = (currentIdx + 1) % len(songs)
		}

		if nextIndex == startIdx {
			return // 已遍历全部，无可播放歌曲
		}

		if err := c.PlayIndex(nextIndex); err == nil {
			return // 成功播放
		}
		// 失败则继续试下一首
		currentIdx = nextIndex
	}
}

// GetStatus 获取当前播放状态
func (c *Controller) GetStatus() Status {
	c.mu.Lock()
	defer c.mu.Unlock()

	var currentSong *playlist.Song
	if c.currentIndex >= 0 && c.currentIndex < len(c.songs) {
		currentSong = &c.songs[c.currentIndex]
	}

	return Status{
		CurrentSong:  currentSong,
		IsPlaying:    c.audioPlayer.IsPlaying(),
		IsPaused:     c.audioPlayer.IsPaused(),
		PlayMode:     c.playMode,
		CurrentIndex: c.currentIndex,
		TotalSongs:   len(c.songs),
		SourceName:   c.sourceName,
	}
}

// GetCurrentIndex 获取当前播放索引
func (c *Controller) GetCurrentIndex() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentIndex
}

// GetSourceName 获取当前播放源名称
func (c *Controller) GetSourceName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sourceName
}

// SetMusicDir 更新音乐目录
func (c *Controller) SetMusicDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.musicDir = dir
}

// SaveState 保存播放状态到文件
func (c *Controller) GetSongs() []playlist.Song {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := make([]playlist.Song, len(c.songs))
	copy(s, c.songs)
	return s
}

// PlayerState 持久化的播放状态
type PlayerState struct {
	MusicDir   string          `json:"music_dir"`
	SourceName string          `json:"source_name"`
	CurrentIdx int             `json:"current_index"`
	PlayMode   PlayMode        `json:"play_mode"`
	AllSongs   []playlist.Song `json:"all_songs"`
}

// SaveState 保存播放状态到文件
func (c *Controller) SaveState(musicDir, stateFile string) error {
	c.mu.Lock()
	// 优先用 Controller 记录的目录（TUI 中切换后的），回退用传入的 flag 值
	dir := c.musicDir
	if dir == "" {
		dir = musicDir
	}
	allSongs := c.playlistMgr.GetAllSongs()
	if len(allSongs) == 0 {
		allSongs = c.songs
	}
	s := PlayerState{
		MusicDir:   dir,
		SourceName: c.sourceName,
		CurrentIdx: c.currentIndex,
		PlayMode:   c.playMode,
		AllSongs:   allSongs,
	}
	c.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		return fmt.Errorf("创建状态目录失败: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}
	// 原子写入：先写临时文件，再重命名
	tmpFile := stateFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("写入状态失败: %w", err)
	}
	return os.Rename(tmpFile, stateFile)
}

// LoadState 从文件加载播放状态
func LoadState(stateFile string) (*PlayerState, error) {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, err
	}
	var s PlayerState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("解析状态失败: %w", err)
	}
	return &s, nil
}
