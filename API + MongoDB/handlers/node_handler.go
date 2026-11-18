package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"mini-cdn/api/repository"
	"mini-cdn/api/services"
)

type NodeHandler struct {
	nodeService *services.NodeService
}

func NewNodeHandler(nodeService *services.NodeService) *NodeHandler {
	return &NodeHandler{
		nodeService: nodeService,
	}
}

type OnboardNodeRequest struct {
	IP        string  `json:"ip" binding:"required"`
	Port      int     `json:"port"`
	Region    string  `json:"region"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// ListNodes handles GET /api/nodes
func (h *NodeHandler) ListNodes(c *gin.Context) {
	nodes, err := h.nodeService.GetAllNodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"nodes":   nodes,
		"total":   len(nodes),
	})
}

// OnboardNode handles POST /api/nodes
func (h *NodeHandler) OnboardNode(c *gin.Context) {
	var req OnboardNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request payload: " + err.Error(),
		})
		return
	}

	params := services.RegisterNodeParams{
		IP:        req.IP,
		Port:      req.Port,
		Region:    req.Region,
		Country:   req.Country,
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
	}

	node, err := h.nodeService.OnboardNode(c.Request.Context(), params)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, services.ErrWorkerUnreachable) {
			status = http.StatusBadGateway
		} else if errors.Is(err, services.ErrDuplicateNode) {
			status = http.StatusConflict
		}

		c.JSON(status, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Node successfully onboarded and activated",
		"node":    node,
	})
}

// GetNode handles GET /api/nodes/:id
func (h *NodeHandler) GetNode(c *gin.Context) {
	id := c.Param("id")
	node, err := h.nodeService.GetNode(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNodeNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error":   "Storage node not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"node":    node,
	})
}

// RemoveNode handles DELETE /api/nodes/:id
func (h *NodeHandler) RemoveNode(c *gin.Context) {
	id := c.Param("id")
	err := h.nodeService.RemoveNode(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNodeNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error":   "Storage node not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Storage node removed successfully",
	})
}

// CheckNodeHealth handles GET /api/nodes/:id/health
func (h *NodeHandler) CheckNodeHealth(c *gin.Context) {
	id := c.Param("id")
	node, err := h.nodeService.CheckNodeHealth(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNodeNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error":   "Storage node not found",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"status":  "OFFLINE",
			"error":   err.Error(),
			"node":    node,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"status":  node.Status,
		"node":    node,
	})
}
