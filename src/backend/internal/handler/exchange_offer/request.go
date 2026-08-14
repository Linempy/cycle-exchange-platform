package exchange_offer

type createBody struct {
	OfferedItemID     int64  `json:"offeredItemId" validate:"required,gt=0"`
	WantedDescription string `json:"wantedDescription" validate:"not_empty,max=5000"`
	WantedCategory    string `json:"wantedCategory" validate:"not_empty,max=100"`
}

type updateBody struct {
	OfferedItemID     int64  `json:"offeredItemId" validate:"required,gt=0"`
	WantedDescription string `json:"wantedDescription" validate:"not_empty,max=5000"`
	WantedCategory    string `json:"wantedCategory" validate:"not_empty,max=100"`
	Version           int64  `json:"version" validate:"required,gt=0"`
}

type deleteQuery struct {
	Version int64 `schema:"version" validate:"required,gt=0"`
}
