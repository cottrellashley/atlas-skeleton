package httperror

import "net/http"

func Fail(w http.ResponseWriter) {
	http.Error(w, "no", http.StatusInternalServerError) // want `http.Error writes text/plain`
}
