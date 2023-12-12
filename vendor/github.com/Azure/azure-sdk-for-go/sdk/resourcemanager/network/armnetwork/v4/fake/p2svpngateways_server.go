//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.
// Code generated by Microsoft (R) AutoRest Code Generator. DO NOT EDIT.
// Changes may cause incorrect behavior and will be lost if the code is regenerated.

package fake

import (
	"context"
	"errors"
	"fmt"
	azfake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/fake/server"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	"net/http"
	"net/url"
	"regexp"
)

// P2SVPNGatewaysServer is a fake server for instances of the armnetwork.P2SVPNGatewaysClient type.
type P2SVPNGatewaysServer struct {
	// BeginCreateOrUpdate is the fake for method P2SVPNGatewaysClient.BeginCreateOrUpdate
	// HTTP status codes to indicate success: http.StatusOK, http.StatusCreated
	BeginCreateOrUpdate func(ctx context.Context, resourceGroupName string, gatewayName string, p2SVPNGatewayParameters armnetwork.P2SVPNGateway, options *armnetwork.P2SVPNGatewaysClientBeginCreateOrUpdateOptions) (resp azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientCreateOrUpdateResponse], errResp azfake.ErrorResponder)

	// BeginDelete is the fake for method P2SVPNGatewaysClient.BeginDelete
	// HTTP status codes to indicate success: http.StatusOK, http.StatusAccepted, http.StatusNoContent
	BeginDelete func(ctx context.Context, resourceGroupName string, gatewayName string, options *armnetwork.P2SVPNGatewaysClientBeginDeleteOptions) (resp azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientDeleteResponse], errResp azfake.ErrorResponder)

	// BeginDisconnectP2SVPNConnections is the fake for method P2SVPNGatewaysClient.BeginDisconnectP2SVPNConnections
	// HTTP status codes to indicate success: http.StatusOK, http.StatusAccepted
	BeginDisconnectP2SVPNConnections func(ctx context.Context, resourceGroupName string, p2SVPNGatewayName string, request armnetwork.P2SVPNConnectionRequest, options *armnetwork.P2SVPNGatewaysClientBeginDisconnectP2SVPNConnectionsOptions) (resp azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientDisconnectP2SVPNConnectionsResponse], errResp azfake.ErrorResponder)

	// BeginGenerateVPNProfile is the fake for method P2SVPNGatewaysClient.BeginGenerateVPNProfile
	// HTTP status codes to indicate success: http.StatusOK, http.StatusAccepted
	BeginGenerateVPNProfile func(ctx context.Context, resourceGroupName string, gatewayName string, parameters armnetwork.P2SVPNProfileParameters, options *armnetwork.P2SVPNGatewaysClientBeginGenerateVPNProfileOptions) (resp azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientGenerateVPNProfileResponse], errResp azfake.ErrorResponder)

	// Get is the fake for method P2SVPNGatewaysClient.Get
	// HTTP status codes to indicate success: http.StatusOK
	Get func(ctx context.Context, resourceGroupName string, gatewayName string, options *armnetwork.P2SVPNGatewaysClientGetOptions) (resp azfake.Responder[armnetwork.P2SVPNGatewaysClientGetResponse], errResp azfake.ErrorResponder)

	// BeginGetP2SVPNConnectionHealth is the fake for method P2SVPNGatewaysClient.BeginGetP2SVPNConnectionHealth
	// HTTP status codes to indicate success: http.StatusOK, http.StatusAccepted
	BeginGetP2SVPNConnectionHealth func(ctx context.Context, resourceGroupName string, gatewayName string, options *armnetwork.P2SVPNGatewaysClientBeginGetP2SVPNConnectionHealthOptions) (resp azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientGetP2SVPNConnectionHealthResponse], errResp azfake.ErrorResponder)

	// BeginGetP2SVPNConnectionHealthDetailed is the fake for method P2SVPNGatewaysClient.BeginGetP2SVPNConnectionHealthDetailed
	// HTTP status codes to indicate success: http.StatusOK, http.StatusAccepted
	BeginGetP2SVPNConnectionHealthDetailed func(ctx context.Context, resourceGroupName string, gatewayName string, request armnetwork.P2SVPNConnectionHealthRequest, options *armnetwork.P2SVPNGatewaysClientBeginGetP2SVPNConnectionHealthDetailedOptions) (resp azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientGetP2SVPNConnectionHealthDetailedResponse], errResp azfake.ErrorResponder)

	// NewListPager is the fake for method P2SVPNGatewaysClient.NewListPager
	// HTTP status codes to indicate success: http.StatusOK
	NewListPager func(options *armnetwork.P2SVPNGatewaysClientListOptions) (resp azfake.PagerResponder[armnetwork.P2SVPNGatewaysClientListResponse])

	// NewListByResourceGroupPager is the fake for method P2SVPNGatewaysClient.NewListByResourceGroupPager
	// HTTP status codes to indicate success: http.StatusOK
	NewListByResourceGroupPager func(resourceGroupName string, options *armnetwork.P2SVPNGatewaysClientListByResourceGroupOptions) (resp azfake.PagerResponder[armnetwork.P2SVPNGatewaysClientListByResourceGroupResponse])

	// BeginReset is the fake for method P2SVPNGatewaysClient.BeginReset
	// HTTP status codes to indicate success: http.StatusOK, http.StatusAccepted
	BeginReset func(ctx context.Context, resourceGroupName string, gatewayName string, options *armnetwork.P2SVPNGatewaysClientBeginResetOptions) (resp azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientResetResponse], errResp azfake.ErrorResponder)

	// BeginUpdateTags is the fake for method P2SVPNGatewaysClient.BeginUpdateTags
	// HTTP status codes to indicate success: http.StatusOK, http.StatusAccepted
	BeginUpdateTags func(ctx context.Context, resourceGroupName string, gatewayName string, p2SVPNGatewayParameters armnetwork.TagsObject, options *armnetwork.P2SVPNGatewaysClientBeginUpdateTagsOptions) (resp azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientUpdateTagsResponse], errResp azfake.ErrorResponder)
}

// NewP2SVPNGatewaysServerTransport creates a new instance of P2SVPNGatewaysServerTransport with the provided implementation.
// The returned P2SVPNGatewaysServerTransport instance is connected to an instance of armnetwork.P2SVPNGatewaysClient via the
// azcore.ClientOptions.Transporter field in the client's constructor parameters.
func NewP2SVPNGatewaysServerTransport(srv *P2SVPNGatewaysServer) *P2SVPNGatewaysServerTransport {
	return &P2SVPNGatewaysServerTransport{
		srv:                                    srv,
		beginCreateOrUpdate:                    newTracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientCreateOrUpdateResponse]](),
		beginDelete:                            newTracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientDeleteResponse]](),
		beginDisconnectP2SVPNConnections:       newTracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientDisconnectP2SVPNConnectionsResponse]](),
		beginGenerateVPNProfile:                newTracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientGenerateVPNProfileResponse]](),
		beginGetP2SVPNConnectionHealth:         newTracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientGetP2SVPNConnectionHealthResponse]](),
		beginGetP2SVPNConnectionHealthDetailed: newTracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientGetP2SVPNConnectionHealthDetailedResponse]](),
		newListPager:                           newTracker[azfake.PagerResponder[armnetwork.P2SVPNGatewaysClientListResponse]](),
		newListByResourceGroupPager:            newTracker[azfake.PagerResponder[armnetwork.P2SVPNGatewaysClientListByResourceGroupResponse]](),
		beginReset:                             newTracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientResetResponse]](),
		beginUpdateTags:                        newTracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientUpdateTagsResponse]](),
	}
}

// P2SVPNGatewaysServerTransport connects instances of armnetwork.P2SVPNGatewaysClient to instances of P2SVPNGatewaysServer.
// Don't use this type directly, use NewP2SVPNGatewaysServerTransport instead.
type P2SVPNGatewaysServerTransport struct {
	srv                                    *P2SVPNGatewaysServer
	beginCreateOrUpdate                    *tracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientCreateOrUpdateResponse]]
	beginDelete                            *tracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientDeleteResponse]]
	beginDisconnectP2SVPNConnections       *tracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientDisconnectP2SVPNConnectionsResponse]]
	beginGenerateVPNProfile                *tracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientGenerateVPNProfileResponse]]
	beginGetP2SVPNConnectionHealth         *tracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientGetP2SVPNConnectionHealthResponse]]
	beginGetP2SVPNConnectionHealthDetailed *tracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientGetP2SVPNConnectionHealthDetailedResponse]]
	newListPager                           *tracker[azfake.PagerResponder[armnetwork.P2SVPNGatewaysClientListResponse]]
	newListByResourceGroupPager            *tracker[azfake.PagerResponder[armnetwork.P2SVPNGatewaysClientListByResourceGroupResponse]]
	beginReset                             *tracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientResetResponse]]
	beginUpdateTags                        *tracker[azfake.PollerResponder[armnetwork.P2SVPNGatewaysClientUpdateTagsResponse]]
}

// Do implements the policy.Transporter interface for P2SVPNGatewaysServerTransport.
func (p *P2SVPNGatewaysServerTransport) Do(req *http.Request) (*http.Response, error) {
	rawMethod := req.Context().Value(runtime.CtxAPINameKey{})
	method, ok := rawMethod.(string)
	if !ok {
		return nil, nonRetriableError{errors.New("unable to dispatch request, missing value for CtxAPINameKey")}
	}

	var resp *http.Response
	var err error

	switch method {
	case "P2SVPNGatewaysClient.BeginCreateOrUpdate":
		resp, err = p.dispatchBeginCreateOrUpdate(req)
	case "P2SVPNGatewaysClient.BeginDelete":
		resp, err = p.dispatchBeginDelete(req)
	case "P2SVPNGatewaysClient.BeginDisconnectP2SVPNConnections":
		resp, err = p.dispatchBeginDisconnectP2SVPNConnections(req)
	case "P2SVPNGatewaysClient.BeginGenerateVPNProfile":
		resp, err = p.dispatchBeginGenerateVPNProfile(req)
	case "P2SVPNGatewaysClient.Get":
		resp, err = p.dispatchGet(req)
	case "P2SVPNGatewaysClient.BeginGetP2SVPNConnectionHealth":
		resp, err = p.dispatchBeginGetP2SVPNConnectionHealth(req)
	case "P2SVPNGatewaysClient.BeginGetP2SVPNConnectionHealthDetailed":
		resp, err = p.dispatchBeginGetP2SVPNConnectionHealthDetailed(req)
	case "P2SVPNGatewaysClient.NewListPager":
		resp, err = p.dispatchNewListPager(req)
	case "P2SVPNGatewaysClient.NewListByResourceGroupPager":
		resp, err = p.dispatchNewListByResourceGroupPager(req)
	case "P2SVPNGatewaysClient.BeginReset":
		resp, err = p.dispatchBeginReset(req)
	case "P2SVPNGatewaysClient.BeginUpdateTags":
		resp, err = p.dispatchBeginUpdateTags(req)
	default:
		err = fmt.Errorf("unhandled API %s", method)
	}

	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchBeginCreateOrUpdate(req *http.Request) (*http.Response, error) {
	if p.srv.BeginCreateOrUpdate == nil {
		return nil, &nonRetriableError{errors.New("fake for method BeginCreateOrUpdate not implemented")}
	}
	beginCreateOrUpdate := p.beginCreateOrUpdate.get(req)
	if beginCreateOrUpdate == nil {
		const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/resourceGroups/(?P<resourceGroupName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways/(?P<gatewayName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)`
		regex := regexp.MustCompile(regexStr)
		matches := regex.FindStringSubmatch(req.URL.EscapedPath())
		if matches == nil || len(matches) < 3 {
			return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
		}
		body, err := server.UnmarshalRequestAsJSON[armnetwork.P2SVPNGateway](req)
		if err != nil {
			return nil, err
		}
		resourceGroupNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("resourceGroupName")])
		if err != nil {
			return nil, err
		}
		gatewayNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("gatewayName")])
		if err != nil {
			return nil, err
		}
		respr, errRespr := p.srv.BeginCreateOrUpdate(req.Context(), resourceGroupNameParam, gatewayNameParam, body, nil)
		if respErr := server.GetError(errRespr, req); respErr != nil {
			return nil, respErr
		}
		beginCreateOrUpdate = &respr
		p.beginCreateOrUpdate.add(req, beginCreateOrUpdate)
	}

	resp, err := server.PollerResponderNext(beginCreateOrUpdate, req)
	if err != nil {
		return nil, err
	}

	if !contains([]int{http.StatusOK, http.StatusCreated}, resp.StatusCode) {
		p.beginCreateOrUpdate.remove(req)
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK, http.StatusCreated", resp.StatusCode)}
	}
	if !server.PollerResponderMore(beginCreateOrUpdate) {
		p.beginCreateOrUpdate.remove(req)
	}

	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchBeginDelete(req *http.Request) (*http.Response, error) {
	if p.srv.BeginDelete == nil {
		return nil, &nonRetriableError{errors.New("fake for method BeginDelete not implemented")}
	}
	beginDelete := p.beginDelete.get(req)
	if beginDelete == nil {
		const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/resourceGroups/(?P<resourceGroupName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways/(?P<gatewayName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)`
		regex := regexp.MustCompile(regexStr)
		matches := regex.FindStringSubmatch(req.URL.EscapedPath())
		if matches == nil || len(matches) < 3 {
			return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
		}
		resourceGroupNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("resourceGroupName")])
		if err != nil {
			return nil, err
		}
		gatewayNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("gatewayName")])
		if err != nil {
			return nil, err
		}
		respr, errRespr := p.srv.BeginDelete(req.Context(), resourceGroupNameParam, gatewayNameParam, nil)
		if respErr := server.GetError(errRespr, req); respErr != nil {
			return nil, respErr
		}
		beginDelete = &respr
		p.beginDelete.add(req, beginDelete)
	}

	resp, err := server.PollerResponderNext(beginDelete, req)
	if err != nil {
		return nil, err
	}

	if !contains([]int{http.StatusOK, http.StatusAccepted, http.StatusNoContent}, resp.StatusCode) {
		p.beginDelete.remove(req)
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK, http.StatusAccepted, http.StatusNoContent", resp.StatusCode)}
	}
	if !server.PollerResponderMore(beginDelete) {
		p.beginDelete.remove(req)
	}

	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchBeginDisconnectP2SVPNConnections(req *http.Request) (*http.Response, error) {
	if p.srv.BeginDisconnectP2SVPNConnections == nil {
		return nil, &nonRetriableError{errors.New("fake for method BeginDisconnectP2SVPNConnections not implemented")}
	}
	beginDisconnectP2SVPNConnections := p.beginDisconnectP2SVPNConnections.get(req)
	if beginDisconnectP2SVPNConnections == nil {
		const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/resourceGroups/(?P<resourceGroupName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways/(?P<p2sVpnGatewayName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/disconnectP2sVpnConnections`
		regex := regexp.MustCompile(regexStr)
		matches := regex.FindStringSubmatch(req.URL.EscapedPath())
		if matches == nil || len(matches) < 3 {
			return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
		}
		body, err := server.UnmarshalRequestAsJSON[armnetwork.P2SVPNConnectionRequest](req)
		if err != nil {
			return nil, err
		}
		resourceGroupNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("resourceGroupName")])
		if err != nil {
			return nil, err
		}
		p2SVPNGatewayNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("p2sVpnGatewayName")])
		if err != nil {
			return nil, err
		}
		respr, errRespr := p.srv.BeginDisconnectP2SVPNConnections(req.Context(), resourceGroupNameParam, p2SVPNGatewayNameParam, body, nil)
		if respErr := server.GetError(errRespr, req); respErr != nil {
			return nil, respErr
		}
		beginDisconnectP2SVPNConnections = &respr
		p.beginDisconnectP2SVPNConnections.add(req, beginDisconnectP2SVPNConnections)
	}

	resp, err := server.PollerResponderNext(beginDisconnectP2SVPNConnections, req)
	if err != nil {
		return nil, err
	}

	if !contains([]int{http.StatusOK, http.StatusAccepted}, resp.StatusCode) {
		p.beginDisconnectP2SVPNConnections.remove(req)
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK, http.StatusAccepted", resp.StatusCode)}
	}
	if !server.PollerResponderMore(beginDisconnectP2SVPNConnections) {
		p.beginDisconnectP2SVPNConnections.remove(req)
	}

	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchBeginGenerateVPNProfile(req *http.Request) (*http.Response, error) {
	if p.srv.BeginGenerateVPNProfile == nil {
		return nil, &nonRetriableError{errors.New("fake for method BeginGenerateVPNProfile not implemented")}
	}
	beginGenerateVPNProfile := p.beginGenerateVPNProfile.get(req)
	if beginGenerateVPNProfile == nil {
		const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/resourceGroups/(?P<resourceGroupName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways/(?P<gatewayName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/generatevpnprofile`
		regex := regexp.MustCompile(regexStr)
		matches := regex.FindStringSubmatch(req.URL.EscapedPath())
		if matches == nil || len(matches) < 3 {
			return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
		}
		body, err := server.UnmarshalRequestAsJSON[armnetwork.P2SVPNProfileParameters](req)
		if err != nil {
			return nil, err
		}
		resourceGroupNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("resourceGroupName")])
		if err != nil {
			return nil, err
		}
		gatewayNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("gatewayName")])
		if err != nil {
			return nil, err
		}
		respr, errRespr := p.srv.BeginGenerateVPNProfile(req.Context(), resourceGroupNameParam, gatewayNameParam, body, nil)
		if respErr := server.GetError(errRespr, req); respErr != nil {
			return nil, respErr
		}
		beginGenerateVPNProfile = &respr
		p.beginGenerateVPNProfile.add(req, beginGenerateVPNProfile)
	}

	resp, err := server.PollerResponderNext(beginGenerateVPNProfile, req)
	if err != nil {
		return nil, err
	}

	if !contains([]int{http.StatusOK, http.StatusAccepted}, resp.StatusCode) {
		p.beginGenerateVPNProfile.remove(req)
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK, http.StatusAccepted", resp.StatusCode)}
	}
	if !server.PollerResponderMore(beginGenerateVPNProfile) {
		p.beginGenerateVPNProfile.remove(req)
	}

	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchGet(req *http.Request) (*http.Response, error) {
	if p.srv.Get == nil {
		return nil, &nonRetriableError{errors.New("fake for method Get not implemented")}
	}
	const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/resourceGroups/(?P<resourceGroupName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways/(?P<gatewayName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)`
	regex := regexp.MustCompile(regexStr)
	matches := regex.FindStringSubmatch(req.URL.EscapedPath())
	if matches == nil || len(matches) < 3 {
		return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
	}
	resourceGroupNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("resourceGroupName")])
	if err != nil {
		return nil, err
	}
	gatewayNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("gatewayName")])
	if err != nil {
		return nil, err
	}
	respr, errRespr := p.srv.Get(req.Context(), resourceGroupNameParam, gatewayNameParam, nil)
	if respErr := server.GetError(errRespr, req); respErr != nil {
		return nil, respErr
	}
	respContent := server.GetResponseContent(respr)
	if !contains([]int{http.StatusOK}, respContent.HTTPStatus) {
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK", respContent.HTTPStatus)}
	}
	resp, err := server.MarshalResponseAsJSON(respContent, server.GetResponse(respr).P2SVPNGateway, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchBeginGetP2SVPNConnectionHealth(req *http.Request) (*http.Response, error) {
	if p.srv.BeginGetP2SVPNConnectionHealth == nil {
		return nil, &nonRetriableError{errors.New("fake for method BeginGetP2SVPNConnectionHealth not implemented")}
	}
	beginGetP2SVPNConnectionHealth := p.beginGetP2SVPNConnectionHealth.get(req)
	if beginGetP2SVPNConnectionHealth == nil {
		const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/resourceGroups/(?P<resourceGroupName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways/(?P<gatewayName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/getP2sVpnConnectionHealth`
		regex := regexp.MustCompile(regexStr)
		matches := regex.FindStringSubmatch(req.URL.EscapedPath())
		if matches == nil || len(matches) < 3 {
			return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
		}
		resourceGroupNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("resourceGroupName")])
		if err != nil {
			return nil, err
		}
		gatewayNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("gatewayName")])
		if err != nil {
			return nil, err
		}
		respr, errRespr := p.srv.BeginGetP2SVPNConnectionHealth(req.Context(), resourceGroupNameParam, gatewayNameParam, nil)
		if respErr := server.GetError(errRespr, req); respErr != nil {
			return nil, respErr
		}
		beginGetP2SVPNConnectionHealth = &respr
		p.beginGetP2SVPNConnectionHealth.add(req, beginGetP2SVPNConnectionHealth)
	}

	resp, err := server.PollerResponderNext(beginGetP2SVPNConnectionHealth, req)
	if err != nil {
		return nil, err
	}

	if !contains([]int{http.StatusOK, http.StatusAccepted}, resp.StatusCode) {
		p.beginGetP2SVPNConnectionHealth.remove(req)
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK, http.StatusAccepted", resp.StatusCode)}
	}
	if !server.PollerResponderMore(beginGetP2SVPNConnectionHealth) {
		p.beginGetP2SVPNConnectionHealth.remove(req)
	}

	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchBeginGetP2SVPNConnectionHealthDetailed(req *http.Request) (*http.Response, error) {
	if p.srv.BeginGetP2SVPNConnectionHealthDetailed == nil {
		return nil, &nonRetriableError{errors.New("fake for method BeginGetP2SVPNConnectionHealthDetailed not implemented")}
	}
	beginGetP2SVPNConnectionHealthDetailed := p.beginGetP2SVPNConnectionHealthDetailed.get(req)
	if beginGetP2SVPNConnectionHealthDetailed == nil {
		const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/resourceGroups/(?P<resourceGroupName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways/(?P<gatewayName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/getP2sVpnConnectionHealthDetailed`
		regex := regexp.MustCompile(regexStr)
		matches := regex.FindStringSubmatch(req.URL.EscapedPath())
		if matches == nil || len(matches) < 3 {
			return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
		}
		body, err := server.UnmarshalRequestAsJSON[armnetwork.P2SVPNConnectionHealthRequest](req)
		if err != nil {
			return nil, err
		}
		resourceGroupNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("resourceGroupName")])
		if err != nil {
			return nil, err
		}
		gatewayNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("gatewayName")])
		if err != nil {
			return nil, err
		}
		respr, errRespr := p.srv.BeginGetP2SVPNConnectionHealthDetailed(req.Context(), resourceGroupNameParam, gatewayNameParam, body, nil)
		if respErr := server.GetError(errRespr, req); respErr != nil {
			return nil, respErr
		}
		beginGetP2SVPNConnectionHealthDetailed = &respr
		p.beginGetP2SVPNConnectionHealthDetailed.add(req, beginGetP2SVPNConnectionHealthDetailed)
	}

	resp, err := server.PollerResponderNext(beginGetP2SVPNConnectionHealthDetailed, req)
	if err != nil {
		return nil, err
	}

	if !contains([]int{http.StatusOK, http.StatusAccepted}, resp.StatusCode) {
		p.beginGetP2SVPNConnectionHealthDetailed.remove(req)
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK, http.StatusAccepted", resp.StatusCode)}
	}
	if !server.PollerResponderMore(beginGetP2SVPNConnectionHealthDetailed) {
		p.beginGetP2SVPNConnectionHealthDetailed.remove(req)
	}

	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchNewListPager(req *http.Request) (*http.Response, error) {
	if p.srv.NewListPager == nil {
		return nil, &nonRetriableError{errors.New("fake for method NewListPager not implemented")}
	}
	newListPager := p.newListPager.get(req)
	if newListPager == nil {
		const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways`
		regex := regexp.MustCompile(regexStr)
		matches := regex.FindStringSubmatch(req.URL.EscapedPath())
		if matches == nil || len(matches) < 1 {
			return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
		}
		resp := p.srv.NewListPager(nil)
		newListPager = &resp
		p.newListPager.add(req, newListPager)
		server.PagerResponderInjectNextLinks(newListPager, req, func(page *armnetwork.P2SVPNGatewaysClientListResponse, createLink func() string) {
			page.NextLink = to.Ptr(createLink())
		})
	}
	resp, err := server.PagerResponderNext(newListPager, req)
	if err != nil {
		return nil, err
	}
	if !contains([]int{http.StatusOK}, resp.StatusCode) {
		p.newListPager.remove(req)
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK", resp.StatusCode)}
	}
	if !server.PagerResponderMore(newListPager) {
		p.newListPager.remove(req)
	}
	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchNewListByResourceGroupPager(req *http.Request) (*http.Response, error) {
	if p.srv.NewListByResourceGroupPager == nil {
		return nil, &nonRetriableError{errors.New("fake for method NewListByResourceGroupPager not implemented")}
	}
	newListByResourceGroupPager := p.newListByResourceGroupPager.get(req)
	if newListByResourceGroupPager == nil {
		const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/resourceGroups/(?P<resourceGroupName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways`
		regex := regexp.MustCompile(regexStr)
		matches := regex.FindStringSubmatch(req.URL.EscapedPath())
		if matches == nil || len(matches) < 2 {
			return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
		}
		resourceGroupNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("resourceGroupName")])
		if err != nil {
			return nil, err
		}
		resp := p.srv.NewListByResourceGroupPager(resourceGroupNameParam, nil)
		newListByResourceGroupPager = &resp
		p.newListByResourceGroupPager.add(req, newListByResourceGroupPager)
		server.PagerResponderInjectNextLinks(newListByResourceGroupPager, req, func(page *armnetwork.P2SVPNGatewaysClientListByResourceGroupResponse, createLink func() string) {
			page.NextLink = to.Ptr(createLink())
		})
	}
	resp, err := server.PagerResponderNext(newListByResourceGroupPager, req)
	if err != nil {
		return nil, err
	}
	if !contains([]int{http.StatusOK}, resp.StatusCode) {
		p.newListByResourceGroupPager.remove(req)
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK", resp.StatusCode)}
	}
	if !server.PagerResponderMore(newListByResourceGroupPager) {
		p.newListByResourceGroupPager.remove(req)
	}
	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchBeginReset(req *http.Request) (*http.Response, error) {
	if p.srv.BeginReset == nil {
		return nil, &nonRetriableError{errors.New("fake for method BeginReset not implemented")}
	}
	beginReset := p.beginReset.get(req)
	if beginReset == nil {
		const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/resourceGroups/(?P<resourceGroupName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways/(?P<gatewayName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/reset`
		regex := regexp.MustCompile(regexStr)
		matches := regex.FindStringSubmatch(req.URL.EscapedPath())
		if matches == nil || len(matches) < 3 {
			return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
		}
		resourceGroupNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("resourceGroupName")])
		if err != nil {
			return nil, err
		}
		gatewayNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("gatewayName")])
		if err != nil {
			return nil, err
		}
		respr, errRespr := p.srv.BeginReset(req.Context(), resourceGroupNameParam, gatewayNameParam, nil)
		if respErr := server.GetError(errRespr, req); respErr != nil {
			return nil, respErr
		}
		beginReset = &respr
		p.beginReset.add(req, beginReset)
	}

	resp, err := server.PollerResponderNext(beginReset, req)
	if err != nil {
		return nil, err
	}

	if !contains([]int{http.StatusOK, http.StatusAccepted}, resp.StatusCode) {
		p.beginReset.remove(req)
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK, http.StatusAccepted", resp.StatusCode)}
	}
	if !server.PollerResponderMore(beginReset) {
		p.beginReset.remove(req)
	}

	return resp, nil
}

func (p *P2SVPNGatewaysServerTransport) dispatchBeginUpdateTags(req *http.Request) (*http.Response, error) {
	if p.srv.BeginUpdateTags == nil {
		return nil, &nonRetriableError{errors.New("fake for method BeginUpdateTags not implemented")}
	}
	beginUpdateTags := p.beginUpdateTags.get(req)
	if beginUpdateTags == nil {
		const regexStr = `/subscriptions/(?P<subscriptionId>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/resourceGroups/(?P<resourceGroupName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)/providers/Microsoft\.Network/p2svpnGateways/(?P<gatewayName>[!#&$-;=?-\[\]_a-zA-Z0-9~%@]+)`
		regex := regexp.MustCompile(regexStr)
		matches := regex.FindStringSubmatch(req.URL.EscapedPath())
		if matches == nil || len(matches) < 3 {
			return nil, fmt.Errorf("failed to parse path %s", req.URL.Path)
		}
		body, err := server.UnmarshalRequestAsJSON[armnetwork.TagsObject](req)
		if err != nil {
			return nil, err
		}
		resourceGroupNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("resourceGroupName")])
		if err != nil {
			return nil, err
		}
		gatewayNameParam, err := url.PathUnescape(matches[regex.SubexpIndex("gatewayName")])
		if err != nil {
			return nil, err
		}
		respr, errRespr := p.srv.BeginUpdateTags(req.Context(), resourceGroupNameParam, gatewayNameParam, body, nil)
		if respErr := server.GetError(errRespr, req); respErr != nil {
			return nil, respErr
		}
		beginUpdateTags = &respr
		p.beginUpdateTags.add(req, beginUpdateTags)
	}

	resp, err := server.PollerResponderNext(beginUpdateTags, req)
	if err != nil {
		return nil, err
	}

	if !contains([]int{http.StatusOK, http.StatusAccepted}, resp.StatusCode) {
		p.beginUpdateTags.remove(req)
		return nil, &nonRetriableError{fmt.Errorf("unexpected status code %d. acceptable values are http.StatusOK, http.StatusAccepted", resp.StatusCode)}
	}
	if !server.PollerResponderMore(beginUpdateTags) {
		p.beginUpdateTags.remove(req)
	}

	return resp, nil
}