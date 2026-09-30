package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createChainRequest struct {
	SampleID string `json:"sample_id"`
	BatchID  string `json:"batch_id"`
	Sampler  string `json:"sampler"`
}

type actorRequest struct {
	Actor string `json:"actor"`
}

type finalizeChainRequest struct {
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
}

func chainID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || id == uuid.Nil {
		writeError(w, http.StatusBadRequest, errors.New("sampling chain id is invalid"))
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) createSamplingChain(w http.ResponseWriter, r *http.Request) {
	var input createChainRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sampleID, err := uuid.Parse(input.SampleID)
	if err != nil || sampleID == uuid.Nil {
		writeError(w, http.StatusBadRequest, errors.New("sample id is invalid"))
		return
	}
	var batchID uuid.UUID
	if input.BatchID != "" {
		batchID, err = uuid.Parse(input.BatchID)
		if err != nil || batchID == uuid.Nil {
			writeError(w, http.StatusBadRequest, errors.New("batch id is invalid"))
			return
		}
	}
	task, err := s.plant.CreateSamplingChain(sampleID, batchID, input.Sampler)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) sealSamplingChain(w http.ResponseWriter, r *http.Request) {
	id, ok := chainID(w, r)
	if !ok {
		return
	}
	var input actorRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.plant.SealSamplingChain(id, input.Actor)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) receiveSamplingChain(w http.ResponseWriter, r *http.Request) {
	id, ok := chainID(w, r)
	if !ok {
		return
	}
	var input actorRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.plant.ReceiveSamplingChain(id, input.Actor)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) archiveSamplingChain(w http.ResponseWriter, r *http.Request) {
	id, ok := chainID(w, r)
	if !ok {
		return
	}
	var input actorRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.plant.ArchiveSamplingChain(id, input.Actor)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) loseSamplingChain(w http.ResponseWriter, r *http.Request) {
	id, ok := chainID(w, r)
	if !ok {
		return
	}
	var input finalizeChainRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.plant.MarkSampleLost(id, input.Actor, input.Reason)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) revokeSamplingChain(w http.ResponseWriter, r *http.Request) {
	id, ok := chainID(w, r)
	if !ok {
		return
	}
	var input finalizeChainRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.plant.RevokeSamplingChain(id, input.Actor, input.Reason)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) samplingChain(w http.ResponseWriter, r *http.Request) {
	id, ok := chainID(w, r)
	if !ok {
		return
	}
	task, err := s.plant.SamplingChain(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) pendingSamplingChains(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"chains": s.plant.PendingChains()})
}

func (s *Server) archivedSamplingChains(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"chains": s.plant.ArchivedChains()})
}

func (s *Server) allSamplingChains(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"chains": s.plant.SamplingChains()})
}
