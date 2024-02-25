package transform

import (
	"encoding/hex"
	"fmt"
	"log"

	"go.uber.org/zap"
	. "m7s.live/engine/v4"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/track"
)

type TransformSubscriber struct {
	Subscriber
	task *TransformTask
}

func (s *TransformSubscriber) Delete() {
	s.Stop(zap.String("reason", "for restart"))
}

func sliceAppend(s1 []byte, s2 []byte) []byte {
	l1 := len(s1)
	l2 := len(s2)

	if cap(s1) < l2+l1 {
		return s1
	}

	for i := 0; i < len(s2); i++ {
		s1[l1+i] = s2[i]
	}

	return s1
}

func (s *TransformSubscriber) OnEvent(event any) {

	//获取转码流发布者
	t := s.task

	//s.Stream.Path
	switch v := event.(type) {
	case *track.Video:
		fmt.Println("=====>  write track.Video to  publisher")
		if s.Video != nil {
			return
		}
		switch v.CodecID {
		case codec.CodecID_H264:
			fmt.Println("=====> CodecID_H264 on sub:", v.PayloadType)
			// vt := p.VideoTrack
			// vt = track.NewH264(p.Stream, v.PayloadType)
			// p.VideoTrack = vt
			log.Printf("pipe in SPS:%d, [%v]\n",
				len(v.ParamaterSets[0]), hex.EncodeToString(v.ParamaterSets[0]))

			log.Printf("pipe in PPS:%d, [%v]\n",
				len(v.ParamaterSets[1]), hex.EncodeToString(v.ParamaterSets[1]))

			//2023/04/02 17:07:51 pipe in SPS:35, [6764001fac2ca4014016ec04400000fa000030d43800001e848000186a02ef2e0fa489]
			//2023/04/02 17:07:51 pipe in PPS:4, [68eb8f2c]
			nal := []byte{0, 0, 0, 1}
			//SPS
			if len(v.ParamaterSets[0]) > 0 {
				//vt.WriteSliceBytes(v.ParamaterSets[0])

				t.writeToFFPipe0(append(nal, v.ParamaterSets[0]...))
			}
			//PPS:
			if len(v.ParamaterSets[1]) > 0 {
				//vt.WriteSliceBytes(v.ParamaterSets[1])
				t.writeToFFPipe0(append(nal, v.ParamaterSets[1]...))
			}
		case codec.CodecID_H265:
			fmt.Println("=====> CodecID_H265 on  sub")
			// vt := p.VideoTrack
			// vt = track.NewH265(p.Stream, v.PayloadType)
			// p.VideoTrack = vt
			// //VPS
			// if len(v.ParamaterSets[0]) > 0 {
			// 	vt.WriteSliceBytes(v.ParamaterSets[0])
			// }
			// //SPS
			// if len(v.ParamaterSets[1]) > 0 {
			// 	vt.WriteSliceBytes(v.ParamaterSets[1])
			// }
			// //PPS:
			// if len(v.ParamaterSets[2]) > 0 {
			// 	vt.WriteSliceBytes(v.ParamaterSets[2])
			// }
		}
		s.AddTrack(v)
	case *track.Audio:
		if s.Audio != nil {
			return
		}
		fmt.Println("=====>  write *track.Audio to  publisher")
		//p.VideoTrack.WriteAnnexB(v.PTS, v.DTS, v.GetAnnexB()[0])
		s.AddTrack(v)
	case VideoFrame:
		//fmt.Println("=====>  write VideoFrame to  publisher")
		firstFrame := v.GetAnnexB()
		// log.Printf("pipe in PTS:%d,DTS:%d buf num:%d\n",
		// 	v.PTS, v.DTS, len(firstFrame))

		for _, buf := range firstFrame {
			// log.Printf("pipe in PTS:%d,DTS:%d buf len:%d,\n",
			// 	v.PTS, v.DTS, len(buf))
			//s.debugPrintfNal(buf, "on sub frame")
			//p.VideoTrack.WriteAnnexB(v.PTS, v.DTS, buf)
			t.writeToFFPipe0(buf)
		}
	case VideoRTP:
		fmt.Println("=====>  on subscribe VideoRTP")
		//p.WritePacketRTP(s.videoTrack, v.Packet)
		//p.VideoTrack.WriteRTPPack(v.Packet)
	case AudioRTP:
		fmt.Println("=====>   on subscribe AudioRTP to")
		//s.stream.WritePacketRTP(s.audioTrack, v.Packet)
		//p.AudioTrack.WriteRTPPack(v.Packet)
	case ISubscriber:
		//代表订阅成功事件，v就是p
		fmt.Println("=====>  begain Subscriber sucess")
	default:
		s.Subscriber.OnEvent(event)

		//fmt.Println("TransformSubscriber OnEvent:%T", v)
	}
}
