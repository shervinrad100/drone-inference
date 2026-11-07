package mlmodels

import "fmt"

type MachineLearningModel struct{}

func (mlModel *MachineLearningModel) Write(p []byte) (n int, err error) {
	// TODO handle the building of frame and emitting it to the ML model
	// TODO logging
	fmt.Printf("%d bytes passed to inference model\n", len(p))
	return len(p), nil
}
