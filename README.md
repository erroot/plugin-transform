# 截图插件
![image](https://github.com/erroot/plugin-transform/blob/master/result1.jpg)


## 插件地址

https://github.com/Monibuca/plugin-transform


http://127.0.0.1:8088/transform?streampath=njtv/njy&transtype=0&newstreampath=njtv/njy-tsh264&videocodec=libx264&osdtext=API动态添加h264转码&resolution=388*256

参数
streampath： 订阅流地址（m7s 内部流地址）
newstreampath：  转码发布的新流地址
videocodec： 转码流编码 libx264 、 libx265
osdtext:  自定义叠加文字 默认“M7S转码”
osdfontcolor: 叠加文字颜色  默认green
osdfontsize： 叠加文字颜色  默认100
osdy: 与osdX配合使用叠加文字位置
osdx: 与osdY配合使用叠加文字位置
osdbox: 叠加背景框  默认0， 1可选
osdboxcolor: 叠加背颜色  默认yellow
resolution： 转码分辨率 格式w*h  eg:720*576


## 插件引入
```go
import (
    _ "m7s.live/plugin/transform/v4"
)
```
## 默认配置

```yaml

transform: 
  ffmpeg: ffmpeg.exe
  fontfile: "SIMHEI.TTF"   #不支持绝对路线需要把系统字符复制到程序执行的相对目录下原因未知
  publishtimeout: 20s 
  onstart:  # 服务启动时自动订阅m7s 系统流进行转码
    -
      streampath: "njtv/glgc"
      resolution: "320*240"
      videocodec: "libx264"   # 当前仅支持libx264 libxh265 转码后发布流无法播放，
      osdfontcolor: "green"
      osdText: "默认配置M7S转码"
      osdbox: 1
      osdboxcolor: "yellow"
    -
      streampath: "live/305"
      resolution: "320*240"
      videocodec: "libx264"
      osdfontcolor: "red"
```
如果ffmpeg无法全局访问，则可修改ffmpeg路径为本地的绝对路径
## API

### `/transform/[streamPath]`


