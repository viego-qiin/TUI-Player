package playlist

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Song 表示一首歌曲
type Song struct {
	FilePath string `json:"file_path"` // 文件绝对路径
	FileName string `json:"file_name"` // 文件名（含扩展名）
	Title    string `json:"title"`     // 歌曲名（不含扩展名）
	Size     int64  `json:"size"`      // 文件大小
}

// Playlist 表示一个歌单
type Playlist struct {
	Name  string `json:"name"`
	Songs []Song `json:"songs"`
}

// Manager 管理所有歌单和全部音乐
type Manager struct {
	mu           sync.RWMutex
	allSongs     []Song
	playlists    map[string]*Playlist
	playlistDir  string
	lastScanDir  string    // 上次扫描的目录
	lastScanTime time.Time // 上次扫描时间（快速缓存）
}

// NewManager 创建新的歌单管理器
func NewManager(playlistDir string) *Manager {
	return &Manager{
		playlists:   make(map[string]*Playlist),
		playlistDir: playlistDir,
	}
}

// ScanMusicDirCached 扫描目录（带缓存：1秒内同一目录不重扫）
func (m *Manager) ScanMusicDirCached(dir string) ([]Song, bool, error) {
	m.mu.RLock()
	if m.lastScanDir == dir && time.Since(m.lastScanTime) < time.Second {
		songs := make([]Song, len(m.allSongs))
		copy(songs, m.allSongs)
		m.mu.RUnlock()
		return songs, true, nil
	}
	m.mu.RUnlock()

	songs, err := m.scanDir(dir)
	if err != nil {
		return nil, false, err
	}
	return songs, false, nil
}

// scanDir 实际扫描目录
func (m *Manager) scanDir(dir string) ([]Song, error) {
	var songs []Song

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".mp3", ".wav", ".ogg", ".flac", ".aac", ".wma", ".m4a":
			song := Song{
				FilePath: path,
				FileName: info.Name(),
				Title:    strings.TrimSuffix(info.Name(), ext),
				Size:     info.Size(),
			}
			songs = append(songs, song)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("扫描音乐目录失败: %w", err)
	}

	sort.Slice(songs, func(i, j int) bool {
		return songs[i].FileName < songs[j].FileName
	})

	m.mu.Lock()
	m.allSongs = songs
	m.lastScanDir = dir
	m.lastScanTime = time.Now()
	m.mu.Unlock()

	return songs, nil
}

// ScanMusicDir 扫描指定目录及其子目录下的所有音乐文件（始终重新扫描）
func (m *Manager) ScanMusicDir(dir string) ([]Song, error) {
	return m.scanDir(dir)
}

// GetAllSongs 获取全部歌曲
func (m *Manager) GetAllSongs() []Song {
	m.mu.RLock()
	defer m.mu.RUnlock()
	songs := make([]Song, len(m.allSongs))
	copy(songs, m.allSongs)
	return songs
}

// SetAllSongs 设置全部歌曲（用于恢复历史状态）
func (m *Manager) SetAllSongs(songs []Song) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allSongs = make([]Song, len(songs))
	copy(m.allSongs, songs)
}

// CreatePlaylist 创建新歌单
func (m *Manager) CreatePlaylist(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.playlists[name]; exists {
		return fmt.Errorf("歌单 '%s' 已存在", name)
	}
	m.playlists[name] = &Playlist{
		Name:  name,
		Songs: make([]Song, 0),
	}
	return nil
}

// DeletePlaylist 删除歌单
func (m *Manager) DeletePlaylist(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.playlists[name]; !exists {
		return fmt.Errorf("歌单 '%s' 不存在", name)
	}
	delete(m.playlists, name)
	return nil
}

// GetPlaylist 获取指定歌单
func (m *Manager) GetPlaylist(name string) (*Playlist, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, exists := m.playlists[name]
	if !exists {
		return nil, fmt.Errorf("歌单 '%s' 不存在", name)
	}
	return p, nil
}

// GetPlaylists 获取所有歌单名称
func (m *Manager) GetPlaylists() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.playlists))
	for name := range m.playlists {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// AddSongToPlaylist 向歌单添加歌曲
func (m *Manager) AddSongToPlaylist(playlistName string, song Song) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, exists := m.playlists[playlistName]
	if !exists {
		return fmt.Errorf("歌单 '%s' 不存在", playlistName)
	}

	// 检查是否已存在
	for _, s := range p.Songs {
		if s.FilePath == song.FilePath {
			return fmt.Errorf("歌曲 '%s' 已在歌单中", song.FileName)
		}
	}

	p.Songs = append(p.Songs, song)
	return nil
}

// RemoveSongFromPlaylist 从歌单移除歌曲
func (m *Manager) RemoveSongFromPlaylist(playlistName string, index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, exists := m.playlists[playlistName]
	if !exists {
		return fmt.Errorf("歌单 '%s' 不存在", playlistName)
	}

	if index < 0 || index >= len(p.Songs) {
		return fmt.Errorf("索引 %d 超出范围（0-%d）", index, len(p.Songs)-1)
	}

	p.Songs = append(p.Songs[:index], p.Songs[index+1:]...)
	return nil
}

// GetPlaylistSongs 获取歌单中的歌曲列表
func (m *Manager) GetPlaylistSongs(playlistName string) ([]Song, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, exists := m.playlists[playlistName]
	if !exists {
		return nil, fmt.Errorf("歌单 '%s' 不存在", playlistName)
	}

	songs := make([]Song, len(p.Songs))
	copy(songs, p.Songs)
	return songs, nil
}

// ShuffleSongs 随机打乱歌曲列表
func ShuffleSongs(songs []Song) []Song {
	result := make([]Song, len(songs))
	copy(result, songs)
	rand.Shuffle(len(result), func(i, j int) {
		result[i], result[j] = result[j], result[i]
	})
	return result
}

// SavePlaylists 将歌单数据保存到文件
func (m *Manager) SavePlaylists() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if err := os.MkdirAll(m.playlistDir, 0755); err != nil {
		return fmt.Errorf("创建歌单目录失败: %w", err)
	}

	for name, pl := range m.playlists {
		filePath := filepath.Join(m.playlistDir, name+".json")
		data, err := json.MarshalIndent(pl, "", "  ")
		if err != nil {
			return fmt.Errorf("序列化歌单 '%s' 失败: %w", name, err)
		}
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return fmt.Errorf("保存歌单 '%s' 失败: %w", name, err)
		}
	}
	return nil
}

// LoadPlaylists 从文件加载歌单数据
func (m *Manager) LoadPlaylists() error {
	if err := os.MkdirAll(m.playlistDir, 0755); err != nil {
		return fmt.Errorf("创建歌单目录失败: %w", err)
	}

	entries, err := os.ReadDir(m.playlistDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取歌单目录失败: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		filePath := filepath.Join(m.playlistDir, entry.Name())

		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("读取歌单文件 '%s' 失败: %w", entry.Name(), err)
		}

		var pl Playlist
		if err := json.Unmarshal(data, &pl); err != nil {
			return fmt.Errorf("解析歌单 '%s' 失败: %w", entry.Name(), err)
		}
		m.playlists[name] = &pl
	}

	return nil
}
