package main

import (
	"github.com/go-flutter-desktop/go-flutter"
	"github.com/go-flutter-desktop/plugins/image_picker"
)

var options = []flutter.Option{
	flutter.WindowInitialDimensions(800, 1280),
	flutter.AddPlugin(&image_picker.ImagePickerPlugin{}),
}
