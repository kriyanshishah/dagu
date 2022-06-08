package admin

import (
	"net/http"

	"github.com/yohamta/dagu/internal/admin/handlers"
)

type route struct {
	method  string
	pattern string
	handler http.HandlerFunc
}

func defaultRoutes(cfg *Config) []*route {
	return []*route{
		{http.MethodGet, `^/?$`, handlers.HandleGetList(
			&handlers.DAGListHandlerConfig{
				DAGsDir: cfg.DAGs,
			},
		)},
		{http.MethodPost, `^/?$`, handlers.HandlePostList(
			&handlers.DAGListHandlerConfig{
				DAGsDir: cfg.DAGs,
			},
		)},
		{http.MethodGet, `^/dags/?$`, handlers.HandleGetList(
			&handlers.DAGListHandlerConfig{
				DAGsDir: cfg.DAGs,
			},
		)},
		{http.MethodGet, `^/views/?$`, handlers.HandleGetViewList()},
		{http.MethodPut, `^/views/?$`, handlers.HandlePutView()},
		{http.MethodGet, `^/views/([^/]+)?$`, handlers.HandleGetView(
			&handlers.ViewHandlerConfig{
				DAGsDir: cfg.DAGs,
			},
		)},
		{http.MethodDelete, `^/views/([^/]+)?$`, handlers.HandleDeleteView()},
		{http.MethodPost, `^/dags/?$`, handlers.HandlePostList(
			&handlers.DAGListHandlerConfig{
				DAGsDir: cfg.DAGs,
			},
		)},
		{http.MethodGet, `^/dags/([^/]+)$`, handlers.HandleGetDAG(
			&handlers.DAGHandlerConfig{
				DAGsDir:            cfg.DAGs,
				LogEncodingCharset: cfg.LogEncodingCharset,
			},
		)},
		{http.MethodPost, `^/dags/([^/]+)$`, handlers.HandlePostDAG(
			&handlers.PostDAGHandlerConfig{
				DAGsDir: cfg.DAGs,
				Bin:     cfg.Command,
				WkDir:   cfg.WorkDir,
			},
		)},
		{http.MethodGet, `^/assets/js/.*$`, handlers.HandleGetAssets(handlers.AssetTypeJs)},
		{http.MethodGet, `^/assets/css/.*$`, handlers.HandleGetAssets(handlers.AssetTypeCss)},
		{http.MethodGet, `^*.woff2$|^*.ttf$`, handlers.HandleGetAssets(handlers.AssetTypeFont)},
	}
}
