package sy_flutter_qiniu_storage

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-flutter-desktop/go-flutter"
	"github.com/go-flutter-desktop/go-flutter/plugin"
	"github.com/qiniu/api.v7/v7/storage"
	"golang.org/x/net/context"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	channelName      = "sy_flutter_qiniu_storage"
	eventChannelName = "sy_flutter_qiniu_storage_event"
)

// SyFlutterQiniuStoragePlugin implements flutter.Plugin and handles method.
type SyFlutterQiniuStoragePlugin struct {
	textureRegistry *flutter.TextureRegistry
	messenger       plugin.BinaryMessenger

	blockCount   int
	upBlockedIds []int // 已上传完成的block_id

	eventSink *plugin.EventSink
}

var _ flutter.Plugin = &SyFlutterQiniuStoragePlugin{} // compile-time type check

type ProgressRecord struct {
	Progresses []storage.BlkputRet `json:"progresses"`
}

var (
	cancelChan chan struct{}
	notifyChan chan struct{}
)

// InitPlugin initializes the plugin.
func (p *SyFlutterQiniuStoragePlugin) InitPlugin(messenger plugin.BinaryMessenger) error {
	cancelChan = make(chan struct{})
	notifyChan = make(chan struct{})
	p.messenger = messenger
	channel := plugin.NewMethodChannel(messenger, channelName, plugin.StandardMethodCodec{})
	channel.HandleFunc("upload", p.upload)
	channel.HandleFunc("cancelUpload", p.cancelUpload)
	eventChannel := plugin.NewEventChannel(p.messenger, eventChannelName, plugin.StandardMethodCodec{})
	eventChannel.Handle(p)
	return nil
}

// InitPluginTexture is used to create and manage backend textures
func (p *SyFlutterQiniuStoragePlugin) InitPluginTexture(registry *flutter.TextureRegistry) error {
	p.textureRegistry = registry
	return nil
}

func (p *SyFlutterQiniuStoragePlugin) upload(arguments interface{}) (reply interface{}, err error) {
	argsMap := arguments.(map[interface{}]interface{})
	fPath := argsMap["filepath"].(string)
	key := argsMap["key"].(string)
	token := argsMap["token"].(string)
	cfg := storage.Config{}
	// 空间对应的机房
	// cfg.Zone = &storage.ZoneHuanan
	// 是否使用https域名
	cfg.UseHTTPS = true
	// 上传是否使用CDN上传加速
	cfg.UseCdnDomains = true
	fileInfo, statErr := os.Stat(fPath)
	if statErr != nil {
		fmt.Println(statErr)
		return responseData(false, key, errors.New("fail to read file:"+fPath+","+statErr.Error()), nil)
	}
	fileSize := fileInfo.Size()
	fileLmd := fileInfo.ModTime().UnixNano()
	recordKey := md5Hex(fmt.Sprintf("%s:%s:%s", key, fPath, fileLmd)) + ".progress"
	recordDir := getCurrentPath() + "/progress"
	mErr := os.MkdirAll(recordDir, 0755)
	if mErr != nil {
		fmt.Println("mkdir for record dir error,", mErr)
		return responseData(false, key, errors.New("mkdir for record dir error,"+mErr.Error()), nil)
	}
	recordPath := filepath.Join(recordDir, recordKey)
	progressRecord := ProgressRecord{}
	// 尝试从旧的进度文件中读取进度
	recordFp, openErr := os.Open(recordPath)
	if openErr == nil {
		progressBytes, readErr := ioutil.ReadAll(recordFp)
		if readErr == nil {
			mErr := json.Unmarshal(progressBytes, &progressRecord)
			if mErr == nil {
				// 检查context 是否过期，避免701错误
				for _, item := range progressRecord.Progresses {
					if storage.IsContextExpired(item) {
						fmt.Println(item.ExpiredAt)
						progressRecord.Progresses = make([]storage.BlkputRet, storage.BlockCount(fileSize))
						break
					}
				}
			}
		}
		recordFp.Close()
	}
	p.upBlockedIds = []int{}
	p.blockCount = storage.BlockCount(fileSize)
	isCancel := false

	if len(progressRecord.Progresses) == 0 {
		progressRecord.Progresses = make([]storage.BlkputRet, storage.BlockCount(fileSize))
	}
	resumeUploader := storage.NewResumeUploader(&cfg)
	ret := storage.PutRet{}
	progressLock := sync.RWMutex{}
	putExtra := storage.RputExtra{
		Progresses: progressRecord.Progresses,
		Notify: func(blkIdx int, blkSize int, ret *storage.BlkputRet) {
			progressLock.Lock()
			progressLock.Unlock()

			if blkSize == int(ret.Offset) && !isCancel {
				p.upBlockedIds = append(p.upBlockedIds, blkIdx)
				notifyChan <- struct{}{}
			}

			//将进度序列化，然后写入文件
			progressRecord.Progresses[blkIdx] = *ret
			progressBytes, _ := json.Marshal(progressRecord)
			wErr := ioutil.WriteFile(recordPath, progressBytes, 0644)
			if wErr != nil {
				fmt.Println("write progress file error,", wErr)
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming integers
	go func() {
		<-cancelChan
		isCancel = true
		fmt.Println("cancel upload")
		cancel()
	}()

	err = resumeUploader.PutFile(ctx, &ret, token, key, fPath, &putExtra)
	if err != nil {
		fmt.Println(err.Error())
		return responseData(false, key, err, ret)
	}
	// 避免上传之后没有接受全 块的回调 增加以下处理
	// fmt.Println("upload over:",len(p.upBlockedIds),",",p.blockCount)
	if len(p.upBlockedIds) < p.blockCount {
		p.upBlockedIds = append(p.upBlockedIds, make([]int, p.blockCount-len(p.upBlockedIds))...)
		notifyChan <- struct{}{}
	}

	//上传成功之后，一定记得删除这个进度文件
	os.Remove(recordPath)

	return responseData(true, key, nil, ret)
}

func md5Hex(str string) string {
	h := md5.New()
	h.Write([]byte(str))
	return hex.EncodeToString(h.Sum(nil))
}

func getCurrentPath() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return strings.Replace(dir, "\\", "/", -1)
}

func responseData(suc bool, key string, err error, ret interface{}) (interface{}, error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	data := map[string]interface{}{
		"success": suc,
		"key":     key,
		"error":   errStr,
		"result":  ret,
	}
	reply, _ := json.Marshal(data)
	return string(reply), err
}

func (p *SyFlutterQiniuStoragePlugin) cancelUpload(arguments interface{}) (reply interface{}, err error) {
	cancelChan <- struct{}{}
	return nil, nil
}

func (p *SyFlutterQiniuStoragePlugin) OnListen(arguments interface{}, sink *plugin.EventSink) { // flutter.EventChannel interface
	p.eventSink = sink

	percent := float64(0)
	for {
		<-notifyChan
		percent = float64(len(p.upBlockedIds)) / float64(p.blockCount)
		sink.Success(percent)
		if len(p.upBlockedIds) == p.blockCount {
			break
		}
	}

	sink.EndOfStream() // !not a complete implementation
}

func (p *SyFlutterQiniuStoragePlugin) OnCancel(arguments interface{}) {}
