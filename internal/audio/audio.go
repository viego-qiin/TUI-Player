package audio

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/go-mp3"
	"github.com/hajimehoshi/oto/v2"
	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
)

type PlaybackState int

const (
	StateStopped PlaybackState = iota
	StatePlaying
	StatePaused
	StateFinished
)

type Player struct {
	mu            sync.Mutex
	state         PlaybackState
	playID        int // 每次 Play() 递增，防止旧 waitDrain 误触发 onFinished
	otoCtx        *oto.Context
	otoPlayer     oto.Player
	stopChan      chan struct{}
	onFinished    func()
	otoSampleRate int
	otoChannels   int
	startTime     time.Time
	pauseTime     time.Time
	totalPause    time.Duration
	totalDuration time.Duration
}

func NewPlayer() (*Player, error) {
	return &Player{state: StateStopped, stopChan: make(chan struct{})}, nil
}

func (p *Player) GetState() PlaybackState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *Player) SetOnFinished(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onFinished = fn
}

func (p *Player) Play(filePath string) error {
	p.mu.Lock()
	p.playID++
	id := p.playID
	p.mu.Unlock()
	p.stopForPlay()

	ext := strings.ToLower(filepathExt(filePath))
	switch ext {
	case ".mp3":
		return p.playMP3(filePath, id)
	case ".flac":
		return p.playFLAC(filePath, id)
	default:
		return fmt.Errorf("不支持的音频格式: %s", ext)
	}
}

// stopForPlay 停止当前播放（不关闭 stopChan，由新播放接管）
func (p *Player) stopForPlay() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == StatePlaying || p.state == StatePaused {
		p.state = StateStopped
		close(p.stopChan)
		if p.otoPlayer != nil {
			p.otoPlayer.Close()
			p.otoPlayer = nil
		}
		p.stopChan = make(chan struct{})
	}
}

func (p *Player) getOtoCtx(sampleRate, channelCount int) (*oto.Context, error) {
	if p.otoCtx != nil && p.otoSampleRate == sampleRate && p.otoChannels == channelCount {
		return p.otoCtx, nil
	}
	if p.otoCtx != nil {
		p.otoCtx.Suspend()
		p.otoCtx = nil
	}
	otoCtx, readyChan, err := oto.NewContext(sampleRate, channelCount, oto.FormatFloat32LE)
	if err != nil {
		return nil, fmt.Errorf("初始化音频输出失败: %w", err)
	}
	<-readyChan
	p.otoCtx = otoCtx
	p.otoSampleRate = sampleRate
	p.otoChannels = channelCount
	return otoCtx, nil
}

// ─── MP3 ───

func (p *Player) playMP3(filePath string, playID int) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	decoded, err := mp3.NewDecoder(f)
	if err != nil {
		f.Close()
		return fmt.Errorf("解码 MP3 失败: %w", err)
	}
	sampleRate := decoded.SampleRate()

	stopCh := make(chan struct{})
	otoCtx, err := p.getOtoCtx(sampleRate, 2)
	if err != nil {
		f.Close()
		return err
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		f.Close()
		return fmt.Errorf("创建 pipe 失败: %w", err)
	}

	p.mu.Lock()
	if p.otoPlayer != nil {
		p.otoPlayer.Close()
	}
	p.state = StatePlaying
	p.stopChan = stopCh
	p.resetTiming()
	p.mu.Unlock()

	// 解码 goroutine：在 Play() 之前启动
	go func() {
		buf := make([]byte, 8192)
		for {
			select {
			case <-stopCh:
				pw.Close()
				pr.Close()
				f.Close()
				return
			default:
			}

			p.mu.Lock()
			paused := p.state == StatePaused
			p.mu.Unlock()
			if paused {
				time.Sleep(50 * time.Millisecond)
				continue
			}

			n, rErr := decoded.Read(buf)
			if n > 0 {
				floatData := int16ToFloat32(buf[:n])
				if _, wErr := pw.Write(floatData); wErr != nil {
					break
				}
			}
			if rErr != nil {
				break
			}
		}
		pw.Close()

		// 等待 oto 播完缓冲区中的数据
		p.waitDrain(stopCh, pr, f, playID)
	}()

	otoPlayer := otoCtx.NewPlayer(pr)
	otoPlayer.Play()
	p.otoPlayer = otoPlayer

	return nil
}

func int16ToFloat32(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	n := len(data) / 2
	out := make([]byte, n*4)
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(data[i*2:]))
		f := float32(s) / 32768.0
		if f > 1.0 {
			f = 1.0
		} else if f < -1.0 {
			f = -1.0
		}
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(f))
	}
	return out
}

// ─── FLAC ───

func (p *Player) playFLAC(filePath string, playID int) error {
	stream, err := flac.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开 FLAC 文件失败: %w", err)
	}
	sr := int(stream.Info.SampleRate)
	cc := int(stream.Info.NChannels)
	bps := int(stream.Info.BitsPerSample)

	stopCh := make(chan struct{})
	otoCtx, err := p.getOtoCtx(sr, cc)
	if err != nil {
		stream.Close()
		return err
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		stream.Close()
		return fmt.Errorf("创建 pipe 失败: %w", err)
	}

	p.mu.Lock()
	if p.otoPlayer != nil {
		p.otoPlayer.Close()
	}
	p.state = StatePlaying
	p.stopChan = stopCh
	p.resetTiming()
	if stream.Info.NSamples > 0 && sr > 0 {
		p.totalDuration = time.Duration(float64(stream.Info.NSamples) / float64(sr) * float64(time.Second))
	}
	p.mu.Unlock()

	// 解码 goroutine：在 Play() 之前启动
	go func() {
		for {
			select {
			case <-stopCh:
				pw.Close()
				pr.Close()
				stream.Close()
				return
			default:
			}

			p.mu.Lock()
			paused := p.state == StatePaused
			p.mu.Unlock()
			if paused {
				time.Sleep(50 * time.Millisecond)
				continue
			}

			frame, fErr := stream.Next()
			if fErr != nil || frame == nil {
				break
			}
			if pErr := frame.Parse(); pErr != nil {
				break
			}

			data := flacFrameToPCM(frame, bps)
			if len(data) == 0 {
				continue
			}

			if _, wErr := pw.Write(data); wErr != nil {
				break
			}
		}
		pw.Close()

		// 等待 oto 播完缓冲区中的数据
		p.waitDrain(stopCh, pr, stream, playID)
	}()

	otoPlayer := otoCtx.NewPlayer(pr)
	otoPlayer.Play()
	p.otoPlayer = otoPlayer

	return nil
}

// waitDrain 等待 oto 播放完管道中剩余的数据，然后触发 onFinished
func (p *Player) waitDrain(stopCh chan struct{}, pr *os.File, cleanup interface{ Close() error }, playID int) {
	// 短暂延迟确保 pw.Close 后的数据被 oto 读取
	time.Sleep(200 * time.Millisecond)

	for {
		select {
		case <-stopCh:
			pr.Close()
			cleanup.Close()
			return
		default:
		}

		p.mu.Lock()
		player := p.otoPlayer
		state := p.state
		currentID := p.playID
		p.mu.Unlock()

		// 如果已经被新播放取代，直接退出
		if currentID != playID {
			pr.Close()
			cleanup.Close()
			return
		}

		if player == nil || state != StatePlaying {
			pr.Close()
			cleanup.Close()
			return
		}

		if !player.IsPlaying() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	pr.Close()
	cleanup.Close()

	// 触发完成回调前再次确认没有新播放
	p.mu.Lock()
	if p.playID == playID && p.state == StatePlaying {
		p.state = StateFinished
		p.otoPlayer = nil
		fn := p.onFinished
		p.mu.Unlock()
		if fn != nil {
			fn()
		}
	} else {
		p.mu.Unlock()
	}
}

func flacFrameToPCM(f *frame.Frame, bps int) []byte {
	if len(f.Subframes) == 0 {
		return nil
	}
	nS := f.Subframes[0].NSamples
	nC := len(f.Subframes)
	maxA := 1 << (bps - 1)
	buf := make([]byte, nS*nC*4)
	idx := 0
	for s := 0; s < nS; s++ {
		for c := 0; c < nC; c++ {
			if s >= len(f.Subframes[c].Samples) {
				continue
			}
			v := float32(f.Subframes[c].Samples[s]) / float32(maxA)
			if v > 1.0 {
				v = 1.0
			} else if v < -1.0 {
				v = -1.0
			}
			binary.LittleEndian.PutUint32(buf[idx:], math.Float32bits(v))
			idx += 4
		}
	}
	return buf[:idx]
}

// ─── 控制 ───

func (p *Player) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.otoPlayer != nil && p.state == StatePlaying {
		p.otoPlayer.Pause()
		p.state = StatePaused
		p.pauseTime = time.Now()
	}
}

func (p *Player) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.otoPlayer != nil && p.state == StatePaused {
		p.otoPlayer.Play()
		p.totalPause += time.Since(p.pauseTime)
		p.state = StatePlaying
	}
}

func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == StatePlaying || p.state == StatePaused {
		p.state = StateStopped
		p.playID++ // 使任何进行中的 waitDrain 失效
		close(p.stopChan)
		if p.otoPlayer != nil {
			p.otoPlayer.Close()
			p.otoPlayer = nil
		}
		p.stopChan = make(chan struct{})
		p.totalDuration = 0
	}
}

func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state == StatePlaying
}

func (p *Player) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state == StatePaused
}

func (p *Player) Elapsed() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == StateStopped || p.startTime.IsZero() {
		return 0
	}
	if p.state == StatePaused {
		return p.pauseTime.Sub(p.startTime) - p.totalPause
	}
	return time.Since(p.startTime) - p.totalPause
}

func (p *Player) TotalDuration() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.totalDuration
}

func (p *Player) resetTiming() {
	p.startTime = time.Now()
	p.pauseTime = time.Time{}
	p.totalPause = 0
}

func (p *Player) Close() {
	p.Stop()
}

func filepathExt(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	return ""
}
