// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tmphost/permahost/cli/internal/client"
)

func newDomainCmd(a *app) *cobra.Command {
	c := &cobra.Command{
		Use:   "domain",
		Short: "Serve a folder on your own domain",
		Long: `Serve a folder on your own domain, such as www.example.com.

Add the domain to the folder, create the CNAME record it prints at your DNS
provider, then verify it. A verified domain is charged a one-time setup fee
and a monthly price in credits ("permahost whoami" shows the prices).

verify and rm take the domain's id or its hostname.`,
		Example: `  permahost domain add --id rs www.example.com
  permahost domain verify --id rs www.example.com
  permahost domain ls --id rs
  permahost domain rm --id rs www.example.com`,
	}
	c.AddCommand(newDomainAddCmd(a), newDomainLsCmd(a), newDomainVerifyCmd(a), newDomainRmCmd(a))
	return c
}

func newDomainAddCmd(a *app) *cobra.Command {
	var id string
	c := &cobra.Command{
		Use:   "add --id ID <hostname>",
		Short: "Add a custom domain to a folder and print its CNAME record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			d, err := c.AddDomain(cmd.Context(), id, args[0])
			if err != nil {
				return err
			}
			return a.output(map[string]any{"domain": d}, func(w io.Writer) {
				fmt.Fprintf(w, "Added %s to folder %s. Create this DNS record:\n\n", d.Hostname, id)
				table(w, "  "+d.Hostname+"\tCNAME\t"+d.Target, nil)
				fmt.Fprintf(w, "\nthen run \"permahost domain verify --id %s %s\".\n", id, d.Hostname)
			})
		},
	}
	c.Flags().StringVar(&id, "id", "", "folder id")
	_ = c.MarkFlagRequired("id")
	return c
}

func newDomainLsCmd(a *app) *cobra.Command {
	var id string
	c := &cobra.Command{
		Use:   "ls --id ID",
		Short: "List a folder's custom domains",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			domains, err := c.ListDomains(cmd.Context(), id)
			if err != nil {
				return err
			}
			return a.output(map[string]any{"domains": domains}, func(w io.Writer) {
				if len(domains) == 0 {
					fmt.Fprintln(w, "No domains.")
					return
				}
				rows := make([]string, len(domains))
				for i, d := range domains {
					rows[i] = d.ID + "\t" + d.Hostname + "\t" + d.Status + "\t" + d.Target + "\t" + d.LastError
				}
				table(w, "ID\tHOSTNAME\tSTATUS\tCNAME TARGET\tLAST ERROR", rows)
			})
		},
	}
	c.Flags().StringVar(&id, "id", "", "folder id")
	_ = c.MarkFlagRequired("id")
	return c
}

func newDomainVerifyCmd(a *app) *cobra.Command {
	var id string
	c := &cobra.Command{
		Use:   "verify --id ID <domain>",
		Short: "Check a domain's CNAME record and activate it",
		Long: `Check a domain's CNAME record now and, when it names the domain's target,
activate the domain. Activating charges the setup fee and the first month.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			domainID, err := resolveDomain(cmd.Context(), c, id, args[0])
			if err != nil {
				return err
			}
			d, err := c.VerifyDomain(cmd.Context(), id, domainID)
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity {
				return fmt.Errorf(`%w: check the CNAME record "permahost domain ls --id %s" shows; DNS changes can take a while to spread`, err, id)
			}
			if err != nil {
				return err
			}
			return a.output(map[string]any{"domain": d}, func(w io.Writer) {
				fmt.Fprintf(w, "%s is %s and serves folder %s.\n", d.Hostname, d.Status, id)
			})
		},
	}
	c.Flags().StringVar(&id, "id", "", "folder id")
	_ = c.MarkFlagRequired("id")
	return c
}

func newDomainRmCmd(a *app) *cobra.Command {
	var id string
	c := &cobra.Command{
		Use:   "rm --id ID <domain>",
		Short: "Remove a custom domain from a folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			domainID, err := resolveDomain(cmd.Context(), c, id, args[0])
			if err != nil {
				return err
			}
			if err := c.DeleteDomain(cmd.Context(), id, domainID); err != nil {
				return err
			}
			return a.output(map[string]any{"deleted": true, "id": id, "domainId": domainID}, func(w io.Writer) {
				fmt.Fprintf(w, "Removed %s from folder %s.\n", args[0], id)
			})
		},
	}
	c.Flags().StringVar(&id, "id", "", "folder id")
	_ = c.MarkFlagRequired("id")
	return c
}

// resolveDomain returns the id of a folder's domain given its id or its
// hostname. Hostnames always contain a dot, which ids never do.
func resolveDomain(ctx context.Context, c *client.Client, folderID, ref string) (string, error) {
	if !strings.Contains(ref, ".") {
		return ref, nil
	}
	domains, err := c.ListDomains(ctx, folderID)
	if err != nil {
		return "", err
	}
	host := strings.TrimSuffix(strings.ToLower(ref), ".")
	for _, d := range domains {
		if d.Hostname == host {
			return d.ID, nil
		}
	}
	return "", fmt.Errorf("folder %s has no domain %s", folderID, ref)
}
