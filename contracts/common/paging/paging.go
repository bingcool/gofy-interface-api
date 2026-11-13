// Package paging defines the shared pagination request and response shapes.
// 包 paging 定义通用的分页请求与响应结构，供各业务契约复用。
package paging

// PageRequest is embedded by paginated requests. It standardises on
// page/page_size so services do not invent size/limit/per_page variants.
// PageRequest 由分页请求嵌入，统一使用 page/page_size 命名，
// 避免各服务自造 size、limit、per_page 之类的变体。
// Page 默认 1、PageSize 默认 20 且上限 10000，由绑定层依据 struct tag 生效。
type PageRequest struct {
	Page     int `form:"page" json:"page" default:"1" validate:"min=1" doc:"页码，从 1 开始" example:"1"`
	PageSize int `form:"page_size" json:"page_size" default:"20" validate:"min=1,max=10000" doc:"每页条数，最大10000" example:"20"`
}

// Offset returns the zero-based row offset.
// Offset 返回从 0 开始的行偏移，可直接用于 SQL OFFSET。
// 它假定 Page 至少为 1，未做下界保护，越界值会算出负偏移。
func (p PageRequest) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// ListData is a list payload. Total is the full count, not the page count.
// ListData 是列表载荷；Total 是满足条件的总条数而非当前页条数，
// 调用方据此计算总页数与是否还有下一页。
type ListData[T any] struct {
	Total int64 `json:"total" doc:"总条数" example:"125"`
	List  []T   `json:"list" doc:"当前页数据"`
}

// PageData is a paginated payload. Page and PageSize echo the request.
// PageData 是分页载荷；Page 与 PageSize 原样回显请求值，
// 请求被默认值或上限修正后，前端仍能对齐真实的分页口径。
type PageData[T any] struct {
	Total    int64 `json:"total" doc:"总条数" example:"125"`
	List     []T   `json:"list" doc:"当前页数据"`
	Page     int   `json:"page" doc:"页码" example:"1"`
	PageSize int   `json:"page_size" doc:"每页条数" example:"20"`
}
