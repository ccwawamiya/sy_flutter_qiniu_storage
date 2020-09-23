# sy_flutter_qiniu_storage

This Go package implements the host-side of the Flutter [sy_flutter_qiniu_storage](https://github.com/ccwawamiya/sy_flutter_qiniu_storage) plugin.

## Usage

Import as:

```go
import sy_flutter_qiniu_storage "github.com/ccwawamiya/sy_flutter_qiniu_storage/go"
```

Then add the following option to your go-flutter [application options](https://github.com/go-flutter-desktop/go-flutter/wiki/Plugin-info):

```go
flutter.AddPlugin(&sy_flutter_qiniu_storage.SyFlutterQiniuStoragePlugin{}),
```
