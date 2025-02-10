package wax_echo

import (
	"io"

	"github.com/michal-laskowski/wax"

	"github.com/labstack/echo/v4"
)

type WaxEchoRenderer struct {
	engine *wax.WaxEngine
}

func NewWaxEchoRenderer(viewResolver wax.ViewResolver, options ...wax.Option) *WaxEchoRenderer {
	return &WaxEchoRenderer{
		engine: wax.New(viewResolver, options...),
	}
}

func (this *WaxEchoRenderer) Render(out io.Writer, view string, model interface{}, context echo.Context) error {
	return this.engine.Render(out, view, model)
}
