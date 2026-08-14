package chain

type Handler struct {
	service chainService
}

func NewHandler(service chainService) *Handler {
	return &Handler{service: service}
}
