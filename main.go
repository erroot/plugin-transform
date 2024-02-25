package transform

import (
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"log"
	"net/http"
	"os/exec"
	"strconv"

	"go.uber.org/zap"
	. "m7s.live/engine/v4"
	"m7s.live/engine/v4/config"
)

//go:embed default.yaml
var defaultYaml DefaultYaml

var tanfsTaskArray = make(map[string]*TransformTask)

type TransformConfig struct {
	DefaultYaml
	config.Publish
	config.Subscribe
	Ffmpeg   string `default:"ffmpeg" desc:"ffmpeg的路径 "`
	Path     string //存储路径
	Filter   string //过滤器
	Fontfile string `default:"shoujin.ttf" desc:"叠加字体路径 "` //osd 叠加字帖路径   shoujin.ttf
	//OnStart  []string `desc:"启动时转码的列表"`                      // 启动时转码的列表

	OnStart []StreamConfig `yaml:"onstart"`
}

// 用此包解析yaml文件到结构体存在三个问题。
//     yaml文件中的配置项首字母不能大写，配置项名字要求比较严格。
//     没有默认值。
//     没有数值校验。

type StreamConfig struct {
	TransType     int    `default:"2" yaml:"transtype"` //转码类型  0: rtsp pull rtmp push; 1: sub raw frame rtmp push; 2: sub raw frame  ts publiser;
	StreamPath    string `default:"" yaml:"streampath"`
	NewStreamPath string `default:"" yaml:"newstreampath"`
	Resolution    string `default:"720*576" yaml:"resolution"`

	VideoCodec string `default:"libx264" yaml:"videocodec"` //libx264, libx265
	Fps        string `default:"25" yaml:"fps"`

	HasOsd       bool   `default:"false" yaml:"hasosd"`
	OsdText      string `default:"M7S 转码" yaml:"osdtext"`
	OsdFontsize  int    `default:"100" yaml:"osdfontsize"`
	OsdFontColor string `default:"white" yaml:"osdfontcolor"`
	OsdX         int    `default:"50" yaml:"osdx"`
	OsdY         int    `default:"50" yaml:"osdy"`
	OsdBox       int    `default:"1" yaml:"osdbox"`
	OsdBoxcolor  string `default:"yellow" yaml:"osdboxcolor"`
}

type TransformTask struct {
	plugin *TransformConfig

	status int //0 :idel ; 1 input ing; 2 output ing

	//统计信息
	restartFFCount int
	rePullCount    int

	streamConfig StreamConfig

	atTime time.Time //开始时间
	cmd    *exec.Cmd

	p *TransformPublisher
	s *TransformSubscriber

	//两个管道一个输入
	in_wp    io.WriteCloser
	in_bytes int

	out_rp    io.ReadCloser
	out_bytes int

	mt sync.Mutex

	f *os.File
}

type TransformPublisher struct {
	TSPublisher
	tsReader *TSReader
	task     *TransformTask
}

func (p *TransformPublisher) Delete() {
	p.Stop()
	if p.tsReader != nil {
		p.tsReader.Close()
	}
}

func (p *TransformPublisher) OnEvent(event any) {
	switch event.(type) {
	case IPublisher:
		p.Stream.NeverTimeout = true
		log.Println("TransformPublisher OnEvent IPublisher...")
		p.TSPublisher.OnEvent(event)
	case SEKick, SEclose:
		log.Println("TransformPublisher SEclose IPublisher...")
		p.Publisher.OnEvent(event)
	default:
		p.Publisher.OnEvent(event)
	}
}

func (t *TransformConfig) OnEvent(event any) {
	switch v := event.(type) {
	case IPublisher:
		log.Println("TransformConfig OnEvent IPublisher...")
	case FirstConfig:
		log.Println("transform FirstConfig")
		for _, stream := range t.OnStart {
			t.SetUpTransformTask(stream)
		}
		break
	case config.Config:
		log.Println("transform config.Config")
		break
	case SEclose:
		log.Println("transform SEclose:%s", v.Target.Path)
		break
		// case SEpublish:
		// 	log.Println("transform SEpublish:%s", v.Stream.Path)
		// 	break
	}
}

var conf = &TransformConfig{
	DefaultYaml: defaultYaml,
}

var TransformPlugin = InstallPlugin(conf)

func (t *TransformConfig) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//streamPath := strings.TrimPrefix(r.RequestURI, "/transform/")

	streamConfig := StreamConfig{
		TransType:     0,
		StreamPath:    "",
		NewStreamPath: "",
		Resolution:    "352*288", //CIF 352*288 qcif 176×144 320*240  1280*720
		Fps:           "25",
		VideoCodec:    "libx264", //libx264,libx265

		HasOsd:       false,
		OsdText:      "M7S 转码",
		OsdFontColor: "green",
		OsdFontsize:  100,
		OsdX:         50,
		OsdY:         50,
		OsdBox:       0,
		OsdBoxcolor:  "yellow",
	}

	streamPath := r.URL.Query().Get("streampath")
	streamConfig.StreamPath = streamPath

	transType, err := strconv.Atoi(r.URL.Query().Get("transtype"))
	if err == nil {
		streamConfig.TransType = transType
	}
	//输出分辨率
	if r.URL.Query().Has("resolution") {
		streamConfig.Resolution = r.URL.Query().Get("resolution")
	}
	//输出编码格式
	if r.URL.Query().Has("videocodec") {
		streamConfig.VideoCodec = r.URL.Query().Get("videocodec")
	}
	//输出分辨率
	if r.URL.Query().Has("fps") {
		streamConfig.Fps = r.URL.Query().Get("fps")
	}

	if r.URL.Query().Has("osdtext") {
		streamConfig.OsdText = r.URL.Query().Get("osdtext")
		streamConfig.HasOsd = true
	}

	if r.URL.Query().Has("osdx") {
		streamConfig.OsdX, err = strconv.Atoi(r.URL.Query().Get("osdx"))
	}
	if r.URL.Query().Has("osdy") {
		streamConfig.OsdY, err = strconv.Atoi(r.URL.Query().Get("osdy"))
	}
	if r.URL.Query().Has("osdboxcolor") {
		streamConfig.OsdBoxcolor = r.URL.Query().Get("osdboxcolor")
	}
	if r.URL.Query().Has("osdbox") {
		streamConfig.OsdBox, err = strconv.Atoi(r.URL.Query().Get("osdbox"))
	}

	if r.URL.Query().Has("newstreampath") {
		streamConfig.NewStreamPath = r.URL.Query().Get("newstreampath")
	}

	t.SetUpTransformTask(streamConfig)

	w.Write([]byte("ok"))
}

func (t *TransformConfig) SetDefaultStreamConfig(config *StreamConfig) {
	// streamConfig := StreamConfig{
	// 	TransType:     0,
	// 	StreamPath:    "",
	// 	NewStreamPath: "",
	// 	Resolution:    "352*288", //CIF 352*288 qcif 176×144 320*240  1280*720
	// 	Fps:           "25",
	// 	VideoCodec:    "libx264", //libx264,libx265

	// 	HasOsd:       false,
	// 	OsdText:      "M7S 转码",
	// 	OsdFontColor: "green",
	// 	OsdFontsize:  100,
	// 	OsdX:         50,
	// 	OsdY:         50,
	// 	OsdBox:       0,
	// 	OsdBoxcolor:  "yellow",
	// }

	if config.Fps == "" {
		config.Fps = "25"
	}

	if config.Resolution == "" {
		config.Resolution = "352*288"
	}

	if config.VideoCodec == "" {
		config.VideoCodec = "libx264"
	}

	if config.OsdText == "" {
		config.OsdText = "M7S 转码"
	}

	if config.OsdFontColor == "" {
		config.OsdFontColor = "green"
	}

	if config.OsdFontsize == 0 {
		config.OsdFontsize = 100
	}

	if config.OsdX == 0 {
		config.OsdX = 100
	}

	if config.OsdY == 0 {
		config.OsdY = 100
	}

	if config.OsdBoxcolor == "" {
		config.OsdBoxcolor = "yellow"
	}

}

func (t *TransformConfig) SetUpTransformTask(config StreamConfig) {
	//更新默认配置
	t.SetDefaultStreamConfig(&config)

	task := &TransformTask{
		plugin:       t,
		streamConfig: config,
	}

	if task.streamConfig.StreamPath == "" {
		TransformPlugin.Info("stream transform invalid\n", zap.String("streamPath", task.streamConfig.StreamPath))
		return
	}

	if task.streamConfig.NewStreamPath == "" {
		typeStr := strconv.FormatInt(int64(task.streamConfig.TransType), 10)
		task.streamConfig.NewStreamPath = task.streamConfig.StreamPath + "-ts" + typeStr
	}

	if tanfsTaskArray[task.streamConfig.NewStreamPath] != nil {
		TransformPlugin.Info("stream transform\n", zap.String("streamPath", task.streamConfig.NewStreamPath))
		return
	}

	task.atTime = time.Now()
	tanfsTaskArray[task.streamConfig.NewStreamPath] = task

	switch task.streamConfig.TransType {
	case 0:
		go task.setupFfmpegTransformThrd0()
	case 1:
		go task.setupFfmpegTransformThrd1()
	case 2:
		go task.setupFfmpegTransformThrd2()
	}

	return

}

// 重点方案，增加ffmpeg 进程异常退出重启功能  默认方法
// 学习stream 码流订阅用法
// 学习stream 码流发布用法
func (t *TransformTask) setupFfmpegTransformThrd0() {
	TransformPlugin.Info("setupFfmpegTransformThrd0 pipe in and out...")

	//添加一个循环 避免ffmpeg 进程异常退出，退出后自动重新启动
	for {
		t.status = 0

		//ffmpeg 启动次数+1
		t.restartFFCount++

		osdText := ""
		//"drawtext=fontsize=100:fontfile=shoujin.ttf:text='m7s转码 ts2':x=500:y=500:fontcolor=green:box=1:boxcolor=yellow",

		if t.streamConfig.OsdText != "" {
			osdText += fmt.Sprintf("drawtext=fontsize=%d:fontfile=%s:text='%s':x=%d:y=%d:fontcolor=%s",
				t.streamConfig.OsdFontsize,
				t.plugin.Fontfile,
				t.streamConfig.OsdText,
				t.streamConfig.OsdX,
				t.streamConfig.OsdY,
				t.streamConfig.OsdFontColor)
		}

		if t.streamConfig.OsdBox != 0 && t.streamConfig.OsdBoxcolor != "" {
			osdText += fmt.Sprintf(":box=1:boxcolor=%s",
				t.streamConfig.OsdBoxcolor)
		}

		TransformPlugin.Info(osdText)

		cmd := exec.Command(conf.Ffmpeg, "-re",
			"-i", "pipe:0",
			"-tune", "zerolatency", //编码延迟参数
			//"-vcodec", t.videoCodec,
			//"-g", "12", "-keyint_min", "12", //设置GOP 大小和关键帧间隔
			//"-b:v", "400k",
			//"-preset", "superfast", //编码延迟参数，superfast ultrafast  影响图像质量
			"-s", t.streamConfig.Resolution,
			"-r", t.streamConfig.Fps,
			"-vf",
			osdText,
			"-c:v", t.streamConfig.VideoCodec,
			//"drawtext=fontsize=100:fontfile=shoujin.ttf:text='m7s转码 ts2':x=500:y=500:fontcolor=green:box=1:boxcolor=yellow",
			//"-acodec", "libfaac",
			//"-b:a", "64k",
			"-acodec", "copy",
			"-f",
			"mpegts", //TS
			"pipe:1",
		)

		TransformPlugin.Info(cmd.String())

		// cmd := exec.Command(conf.Ffmpeg, "-re",
		// 	"-i", "pipe:0",
		// 	"-tune", "zerolatency", //编码延迟参数
		// 	"-vcodec", "libx264",
		// 	"-g", "12", "-keyint_min", "12", //设置GOP 大小和关键帧间隔
		// 	"-b:v", "400k",
		// 	"-preset", "superfast", //编码延迟参数，superfast ultrafast  影响图像质量
		// 	"-s", "320*240",
		// 	"-r", "25",
		// 	"-vf",
		// 	"drawtext=fontsize=100:fontfile=shoujin.ttf:text='m7s转码 ts2':x=500:y=500:fontcolor=green:box=1:boxcolor=yellow",
		// 	"-acodec", "libfaac",
		// 	"-b:a", "64k",
		// 	"-f",
		// 	//"h264",
		// 	//"flv",
		// 	"mpegts", //TS
		// 	//"mpeg", //ps
		// 	"pipe:1",
		// 	//"-y",
		// 	//"transform.flv",
		// )

		//获取输入流
		stdin, err := cmd.StdinPipe()
		if err != nil {
			TransformPlugin.Error("Error getting stdin pipe:", zap.Error(err))
			continue
		}

		t.cmd = cmd

		t.in_wp = stdin

		//获取输出流 句柄
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			TransformPlugin.Error("Error getting stdout pipe:", zap.Error(err))
			continue
		}
		t.out_rp = stdout

		// Start the command
		err = cmd.Start()
		if err != nil {
			TransformPlugin.Error("Error starting command:", zap.Error(err))
			continue
		}

		//优先启动读管道数据进程
		go t.readFFPipe1AndToPublisher()

		//定义一个订阅者
		s := &TransformSubscriber{}
		//s.IsInternal = true
		s.task = t
		t.s = s

		if err := TransformPlugin.Subscribe(t.streamConfig.StreamPath, s); err != nil {
			TransformPlugin.Error("TransformPlugin 2 Subscribe faild")
			continue
		} else {
			//重点需要goroutin  启动订阅流，且只订阅了video track 裸流
			//避免重复请求播放
			if !s.IsPlaying() {
				TransformPlugin.Info("TransformPlugin Subscribe sucess 2 play")
				go s.PlayRaw()
			}
		}

		TransformPlugin.Info("cmd Start  wait end....\n")
		err = cmd.Wait()
		if err != nil {
			TransformPlugin.Error("Error Wait command:", zap.Error(err))
		}
		//复位读写指针
		t.out_rp = nil
		t.in_wp = nil

		//关闭订阅流
		if t.s != nil {
			TransformPlugin.Info("try to close TransformSubscriber")
			t.s.Delete()
			t.s = nil
		}
		//关闭发布流
		if t.p != nil {
			t.p.Delete()
			t.p = nil
			TransformPlugin.Info("try to close TransformPublisher")
		}
		TransformPlugin.Info("ffmpegTransformThrd end to restart", zap.Int("restartFFCount", t.restartFFCount))

		//延迟后重启
		time.Sleep(time.Duration(1000) * time.Millisecond)

	}

	//TransformPlugin.Info("ffmpeg task end...")

	//t.taskEnd("ffmpeg cmd end")
}

// 订阅的Track数据写入ffmpeg 输入管道
func (t *TransformTask) writeToFFPipe0(buf []byte) {

	//左闭右开
	// n := len(buf)
	// if n > 5 && binary.BigEndian.Uint32(buf[0:5]) == 1 && buf[4]&0x1f == 7 {
	// 	log.Printf("pipe in SPS:%d, [%v]\n",
	// 		len(buf), hex.EncodeToString(buf))
	// } else if n > 5 && binary.BigEndian.Uint32(buf[0:5]) == 1 && buf[4]&0x1f == 8 {
	// 	log.Printf("pipe in PPS:%d, [%v]\n",
	// 		len(buf), hex.EncodeToString(buf))
	// } else {
	// 	if n > 5 {
	// 		n = 5
	// 	}
	// 	log.Printf("pipe in raw:%d, [%v]\n",
	// 		len(buf), hex.EncodeToString(buf[0:n]))
	// }

	t.status = 1
	if t.in_wp == nil {
		TransformPlugin.Warn("invalid in pipe wp")
		return
	}
	t.in_bytes += len(buf)
	_, err := t.in_wp.Write(buf)
	if err != nil {
		TransformPlugin.Error("write to pipe0 failed:", zap.Error(err))
	}
}

// ffmpeg 转码后的ts 流  发布 stream
func (t *TransformTask) readFFPipe1AndToPublisher() {
	//s.readTsDataFromPipeOut()

	if t.out_rp == nil {
		//time.Sleep(time.Duration(50) * time.Microsecond)
		//continue
		return
	}

	if t.p == nil {
		//发布一个新的转码流
		//定义一个发布者
		p := &TransformPublisher{}
		p.task = t
		t.p = p

		//判断流是否存在，存在则删除重新发布
		s := Streams.Get(t.streamConfig.NewStreamPath)
		if s != nil {
			Streams.Delete(t.streamConfig.NewStreamPath)
		}
		TransformPlugin.Info("TransformTask TSPublisher", zap.String("newStreamPath", t.streamConfig.NewStreamPath))
		if err := TransformPlugin.Publish(t.streamConfig.NewStreamPath, p); err != nil {
			TransformPlugin.Error("TransformTask publish:", zap.Error(err))
			return
		}
		p.AudioTrack = nil
		p.VideoTrack = nil

		//buf := make([]byte, 64*1024)
		p.tsReader = NewTSReader(&p.TSPublisher)
		//defer tsReader.Close()

		//很重要这一步
		//ffmpeg restart t.out_rp 会发生变化
		if p.TSPublisher.IO.Reader != t.out_rp {
			TransformPlugin.Info("TSPublisher SetIO...")
			p.TSPublisher.SetIO(t.out_rp)
		}
	}

	p := t.p

	for {
		if t.out_rp == nil {
			TransformPlugin.Info("TransformTask TSPublisher no out rp valid exit thrd")
			break
		}
		//err := tsReader.Feed(t.out_rp)
		//很重要这一步
		if p != nil && p.tsReader != nil {
			err := p.tsReader.Feed(p)
			if err == errors.New("file already closed") {
				TransformPlugin.Error("tsReader.Feed:", zap.Error(err))
				break
			}
			if err != nil {
				TransformPlugin.Error("tsReader.Feed:", zap.Error(err))
				//避免循环快速打印
				time.Sleep(time.Duration(500) * time.Millisecond)
				//
			}

		}

		//log.Println("p end Feed...")
	}

}

// 为了验证测试，使用者自己加固，异常处理
func (t *TransformTask) setupFfmpegTransformThrd1() {
	TransformPlugin.Info("setupFfmpegTransformThrd1 url in and rtmp out...")
	//转码并缩放
	//成功
	//ffmpeg -ss 0:01 -i "rtsp://127.0.0.1:554/njtv/glgc" -vcodec copy  -vcodec libx264 -s 720*576 -f flv "rtmp://127.0.0.1:1935/njtv/glgc-d1"

	pullpath := "rtsp://127.0.0.1:554/" + t.streamConfig.StreamPath
	tspath := "rtmp://127.0.0.1:1935/" + t.streamConfig.NewStreamPath

	//转码并保存
	//cmd := exec.Command("ffmpeg", "-re", "-r", "30", "-i", "pipe:0", "-vcodec", "libx264", "-f", "flv", "pipe:1", "-y", "another.flv")
	cmd := exec.Command(conf.Ffmpeg, "-re",
		"-i", pullpath,
		//"-vcodec", "copy",
		"-tune", "zerolatency", //编码延迟参数
		"-vcodec", "libx264",
		"-g", "12", "-keyint_min", "12", //设置GOP 大小和关键帧间隔
		"-b:v", "400k",
		"-preset", "superfast", //编码延迟参数，superfast ultrafast  影响图像质量
		"-s", "320*240",
		"-r", "25",
		"-vf",
		"drawtext=fontsize=100:fontfile=shoujin.ttf:text='m7s转码 ts0':x=500:y=500:fontcolor=green:box=1:boxcolor=yellow",
		"-acodec", "libfaac",
		"-b:a", "64k",
		"-f",
		"flv",
		tspath,
	)

	t.cmd = cmd
	// Start the command
	err := cmd.Start()
	if err != nil {
		fmt.Println("Error starting command:", err)
		return
	}
	log.Printf("cmd Start  wait end....\n")
	err = cmd.Wait()
	if err != nil {
		fmt.Println("Error Wait command:", err)
	}
	log.Printf("ffmpegTransformThrd end\n")

	t.taskEnd("cmd end")
}

// 为了验证测试，使用者自己加固，异常处理
func (t *TransformTask) setupFfmpegTransformThrd2() {
	TransformPlugin.Info("setupFfmpegTransformThrd2 pipe in and rtmp out...")
	s := &TransformSubscriber{}
	//s.IsInternal = true
	s.task = t

	t.s = s

	// sub.onSubscriberSucess()
	if err := TransformPlugin.Subscribe(t.streamConfig.StreamPath, s); err != nil {
		TransformPlugin.Error("TransformPlugin 1 Subscribe faild")
		t.taskEnd("Subscribe faild")
		return
	} else {
		TransformPlugin.Info("TransformPlugin Subscribe sucess")
		//重点需要goroutin
		go s.PlayRaw()
	}

	cmd := exec.Command(conf.Ffmpeg, "-re",
		"-i", "pipe:0",
		"-tune", "zerolatency", //编码延迟参数
		"-vcodec", "libx264",
		"-g", "12", "-keyint_min", "12", //设置GOP 大小和关键帧间隔
		"-b:v", "400k",
		"-preset", "superfast", //编码延迟参数，superfast ultrafast  影响图像质量
		"-s", "320*240",
		"-r", "25",
		"-vf",
		"drawtext=fontsize=100:fontfile=shoujin.ttf:text='m7s转码 ts1':x=500:y=500:fontcolor=green:box=1:boxcolor=yellow",
		"-acodec", "libfaac",
		"-b:a", "64k",
		"-f",
		"flv",
		"rtmp://127.0.0.1:1935/"+t.streamConfig.NewStreamPath,
	)

	t.cmd = cmd
	//获取输入流
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Println("Error getting stdin pipe:", err)
		return
	}
	t.in_wp = stdin

	// Start the command
	err = cmd.Start()
	if err != nil {
		fmt.Println("Error starting command:", err)
		return
	}
	log.Printf("cmd Start  wait end....\n")
	err = cmd.Wait()
	if err != nil {
		fmt.Println("Error Wait command:", err)
	}
	log.Printf("ffmpegTransformThrd end\n")

	t.taskEnd("cmd end")
}

func (t *TransformTask) taskEnd(reason string) {

	tanfsTaskArray[t.streamConfig.NewStreamPath] = nil

	if t.p != nil {
		t.p.Stop()
		t.p = nil
	}
	if t.s != nil {
		t.s.Stop()
		t.s = nil
	}

	log.Printf(fmt.Sprintf("task:%s end for:%s\n", t.streamConfig.NewStreamPath, reason))
}

func (t *TransformTask) debugPrintfNal(buf []byte, name string) {
	n := len(buf)
	if n > 5 {
		n = 5
	}
	log.Printf("nal %s out len:%d, [%v]\n",
		name,
		len(buf), hex.EncodeToString(buf[0:n]))

	// s.Info("nal :",
	// 	zap.String("int_bytes", name),
	// 	zap.Int("int_bytes", len(buf)),
	// 	zap.String("int_bytes", hex.EncodeToString(buf[0:n])))
}

func (t *TransformTask) writeTmpFile(data []byte) {

	if t.f == nil {
		f, err := os.Create("tranform-tmp.ps")
		if f == nil {
			log.Println("create file faild:%v", err)
			return
		}
		t.f = f
	}

	_, err2 := t.f.Write(data)
	if err2 != nil {
		log.Println("Write file faild:%v", err2)
		return
	}

	if t.out_bytes > 1024*1024*5 {
		t.f.Close()
		os.Exit(0)
	}

}

/*
func (s *TransformSubscriber) OnEvent(event any) {
	switch v := event.(type) {
	case VideoFrame:
		//s.Stop()
		// var errOut util.Buffer
		// firstFrame := v.GetAnnexB()
		// cmd := exec.Command(conf.FFmpeg, "-hide_banner", "-i", "pipe:0", "-vframes", "1", "-f", "mjpeg", "pipe:1")
		// cmd.Stdin = &firstFrame
		// cmd.Stderr = &errOut
		// cmd.Stdout = s
		// cmd.Run()
		// if errOut.CanRead() {
		// 	s.Info(string(errOut))
		// }
	default:
		s.Subscriber.OnEvent(event)
	}
}*/
