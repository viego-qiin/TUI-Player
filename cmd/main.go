package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/qiinY/tui-player/internal/audio"
	"github.com/qiinY/tui-player/internal/player"
	"github.com/qiinY/tui-player/internal/playlist"
	"github.com/qiinY/tui-player/internal/ui"
)

func main() {
	musicDir := flag.String("d", ".", "音乐文件目录")
	flag.Parse()

	if info, err := os.Stat(*musicDir); err != nil {
		log.Fatalf("无法访问目录 %s: %v", *musicDir, err)
	} else if !info.IsDir() {
		log.Fatalf("%s 不是有效的目录", *musicDir)
	}

	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("获取程序路径失败: %v", err)
	}
	dataDir := filepath.Join(filepath.Dir(execPath), "data")
	stateFile := filepath.Join(dataDir, "state.json")

	playlistMgr := playlist.NewManager(filepath.Join(dataDir, "playlists"))
	if err := playlistMgr.LoadPlaylists(); err != nil {
		log.Printf("加载歌单失败: %v", err)
	}

	// 尝试加载上次状态
	var prevState *player.PlayerState
	if st, err := player.LoadState(stateFile); err == nil {
		prevState = st
		// 如果上次有不同目录且用户没指定，用上次的目录
		if *musicDir == "." && st.MusicDir != "" && st.MusicDir != "." {
			*musicDir = st.MusicDir
			if info, err := os.Stat(*musicDir); err != nil || !info.IsDir() {
				*musicDir = "." // 回退
			}
		}
	}

	// 扫描音乐文件夹
	songs, err := playlistMgr.ScanMusicDir(*musicDir)
	if err != nil {
		log.Printf("扫描音乐目录失败: %v", err)
	}

	audioPlayer, err := audio.NewPlayer()
	if err != nil {
		log.Fatalf("初始化音频播放器失败: %v", err)
	}
	defer audioPlayer.Close()

	playerCtrl := player.NewController(audioPlayer, playlistMgr)
	playerCtrl.SetMusicDir(*musicDir)

	// 恢复上次播放状态：历史记录歌曲数更多时优先恢复
	if prevState != nil && len(prevState.AllSongs) > len(songs) {
		songs = prevState.AllSongs
		playlistMgr.SetAllSongs(songs)
	}
	if len(songs) > 0 {
		playerCtrl.SetPlaylist(songs, "全部音乐")
		if prevState != nil {
			playerCtrl.SetPlayMode(prevState.PlayMode)
			playerCtrl.SetCurrentIndex(prevState.CurrentIdx)
			if prevState.MusicDir != "" {
				playerCtrl.SetMusicDir(prevState.MusicDir)
			}
		}
	}

	// 启动 TUI（退出前始终保存，无论 TUI 是否异常）
	runErr := ui.Run(playlistMgr, playerCtrl, audioPlayer, *musicDir)

	// 退出前保存
	if err := playlistMgr.SavePlaylists(); err != nil {
		log.Printf("保存歌单失败: %v", err)
	}
	if err := playerCtrl.SaveState(*musicDir, stateFile); err != nil {
		log.Printf("保存播放状态失败: %v", err)
	}
	log.Printf("状态已保存到 %s", stateFile)

	if runErr != nil {
		log.Fatalf("TUI 运行失败: %v", runErr)
	}
}
