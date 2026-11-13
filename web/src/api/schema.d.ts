// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
// Code generated from api/openapi.yaml; DO NOT EDIT.

export interface paths {
    "/api/v1/about": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Version of the running server and the link to its source */
        get: operations["getAbout"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/live": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * WebSocket with push events of the current user
         * @description WebSocket upgrade (RFC 6455). Requires the session cookie and an Origin header equal to BRAID_PUBLIC_URL.
         *     Frames are server-to-client JSON text and carry ids only, never data; client frames are ignored
         *     (at most 256 bytes each, more than 20 per 10 s closes the socket). Frames:
         *     `{"type":"hello","protocol":1}` right after the upgrade; `{"type":"ping"}` every 25 s;
         *     `{"type":"session_updated","session_id":"..."}`; `{"type":"device_synced","device_id":"..."}`;
         *     `{"type":"reindex","state":"started"|"done"}`; `{"type":"resync"}` (events were lost, refetch everything).
         *     Close codes: 1001 server shutdown, 4401 session no longer valid, 4429 client sent too many frames.
         *     At most 10 connections per user and 30 connection attempts per minute.
         */
        get: operations["liveSocket"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/me": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Current user */
        get: operations["getMe"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /** Set the user's time zone */
        patch: operations["patchMe"];
        trace?: never;
    };
    "/api/v1/devices": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Own devices */
        get: operations["listDevices"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/devices/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["Id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        /** Revoke a device and all its tokens */
        delete: operations["revokeDevice"];
        options?: never;
        head?: never;
        /** Rename a device */
        patch: operations["renameDevice"];
        trace?: never;
    };
    "/api/v1/audit": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Own audit entries */
        get: operations["listAudit"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/admin/users": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** All users (admin) */
        get: operations["adminListUsers"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/admin/users/{id}/disable": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["Id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Disable a user; revokes all sessions and tokens */
        post: operations["adminDisableUser"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/admin/users/{id}/enable": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["Id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Enable a user */
        post: operations["adminEnableUser"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/admin/users/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["Id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        /** Delete a user and all their data */
        delete: operations["adminDeleteUser"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/admin/audit": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Instance-wide audit log (admin) */
        get: operations["adminListAudit"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/stats/live": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Live sessions with their active agents and current tools */
        get: operations["statsLive"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/stats/filters": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Distinct projects, models and skills of the sessions in range */
        get: operations["statsFilters"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/stats/overview": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Overview KPIs, series and heatmap */
        get: operations["statsOverview"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/stats/sessions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Sessions in range, newest first */
        get: operations["statsSessions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/stats/sessions/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** One session with agents, skill phases and failures */
        get: operations["statsSession"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/stats/sessions/{id}/lines": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Transcript lines of one agent of a session */
        get: operations["statsSessionLines"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/stats/failures": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Failures with aggregates and time lost */
        get: operations["statsFailures"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/stats/search": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Full-text search over prompts and replies */
        get: operations["statsSearch"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/admin/stats": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Per-user derived statistics and totals (admin) */
        get: operations["adminStats"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v1/stats/self": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Today's figures and one session, for the device (statusline) */
        get: operations["statsSelf"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        Error: {
            error: string;
        };
        About: {
            version: string;
            commit: string;
            /**
             * Format: uri
             * @description Link to exactly this version.
             */
            source_url: string;
        };
        Me: {
            /** Format: uuid */
            id: string;
            email: string;
            name: string;
            is_admin: boolean;
            /** @description Effective zone (UTC when unset). */
            time_zone: string;
            time_zone_set: boolean;
        };
        Device: {
            /** Format: uuid */
            id: string;
            name: string;
            hostname: string;
            os: string;
            arch: string;
            os_user: string;
            daemon_version: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            last_seen_at: string | null;
            /** Format: date-time */
            last_sync_at: string | null;
            /** Format: date-time */
            revoked_at: string | null;
            /** @description Not revoked and last sync (or creation, if never synced) older than 7 days. */
            stale: boolean;
        };
        AuditEntry: {
            /** Format: int64 */
            id: number;
            /** Format: date-time */
            at: string;
            action: string;
            /** Format: uuid */
            actor_user_id: string | null;
            /** Format: uuid */
            subject_user_id: string | null;
            /** Format: uuid */
            device_id: string | null;
            ip: string | null;
            user_agent: string | null;
            detail: Record<string, never> | null;
        };
        AuditPage: {
            entries: components["schemas"]["AuditEntry"][];
            /**
             * Format: int64
             * @description Cursor for the next page (pass as before), null on the last page.
             */
            next: number | null;
        };
        AdminUser: {
            /** Format: uuid */
            id: string;
            email: string;
            name: string;
            is_admin: boolean;
            /** Format: date-time */
            disabled_at: string | null;
            /** Format: date-time */
            last_login_at: string | null;
            /** Format: date-time */
            last_sync_at: string | null;
            devices: number;
            /** Format: int64 */
            stored_bytes: number;
        };
        Overview: {
            /** Format: date-time */
            from: string;
            /** Format: date-time */
            to: string;
            time_zone: string;
            reindexing: boolean;
            kpi: components["schemas"]["KPI"];
            per_day: components["schemas"]["DayPoint"][];
            per_model: components["schemas"]["ModelPoint"][];
            per_project: components["schemas"]["ProjectPoint"][];
            heatmap: components["schemas"]["HeatCell"][];
            per_day_by_project: components["schemas"]["ProjectSplit"];
        };
        /** @description Daily cost and tokens by project; the top 8 projects by cost plus a trailing "_other" entry when more exist. days[].values[i] belongs to projects[i]. */
        ProjectSplit: {
            projects: {
                key: string;
                name: string;
            }[];
            days: {
                /** Format: date */
                day: string;
                values: {
                    cost_usd: number;
                    /** Format: int64 */
                    tokens: number;
                }[];
            }[];
        };
        KPI: {
            /** Format: int64 */
            sessions: number;
            /** Format: int64 */
            agent_ms: number;
            /** Format: int64 */
            total_agent_ms: number;
            agent_ms_estimated: boolean;
            cost_usd: number;
            cost_unknown_models: string[];
            /** Format: int64 */
            tokens: number;
            streak_days: number;
            /** Format: int64 */
            failures: number;
            /** Format: int64 */
            time_lost_s: number;
        };
        DayPoint: {
            /** Format: date */
            day: string;
            cost_usd: number;
            /** Format: int64 */
            tokens: number;
            /** Format: int64 */
            sessions: number;
        };
        ModelPoint: {
            model: string;
            cost_usd: number;
            /** Format: int64 */
            tokens: number;
            /** Format: int64 */
            messages: number;
            price_unknown: boolean;
        };
        ProjectPoint: {
            path: string;
            name: string;
            cost_usd: number;
            /** Format: int64 */
            tokens: number;
            /** Format: int64 */
            sessions: number;
            /** Format: int64 */
            agent_ms: number;
        };
        HeatCell: {
            weekday: number;
            hour: number;
            /** Format: int64 */
            count: number;
        };
        SessionItem: {
            session_id: string;
            title: string;
            project: string;
            project_name: string;
            /** Format: uuid */
            device_id: string | null;
            device_ids: string[];
            models: string[];
            /** Format: date-time */
            started_at: string;
            /** Format: date-time */
            ended_at: string;
            is_live: boolean;
            /** Format: int64 */
            agent_ms: number;
            /** Format: int64 */
            total_agent_ms: number;
            agent_ms_estimated: boolean;
            cost_usd: number;
            /** Format: int64 */
            tokens: number;
            /** Format: int64 */
            failures: number;
            skills: string[];
            /** Format: int64 */
            line_count: number;
            git_branch: string;
        };
        CurrentTool: {
            name: string;
            summary: string;
            /** Format: date-time */
            started_at: string;
        };
        LiveAgent: {
            agent_id: string;
            agent_type: string;
            description: string;
            parent_agent_id: string | null;
            /** Format: date-time */
            started_at: string | null;
            /** @description Newest tool call of the agent without end, started less than 15 minutes ago. */
            current_tool: components["schemas"]["CurrentTool"] | null;
        };
        LiveSession: {
            session: components["schemas"]["SessionItem"];
            /** @description Agents active within 2 minutes */
            agents: components["schemas"]["LiveAgent"][];
        };
        LivePage: {
            items: components["schemas"]["LiveSession"][];
        };
        ProjectOption: {
            path: string;
            name: string;
        };
        FilterOptions: {
            projects: components["schemas"]["ProjectOption"][];
            models: string[];
            skills: string[];
        };
        SessionPage: {
            items: components["schemas"]["SessionItem"][];
            next: string | null;
        };
        AgentUsage: {
            cost_usd: number;
            /** Format: int64 */
            tokens: number;
            /** Format: int64 */
            input: number;
            /** Format: int64 */
            output: number;
            /** Format: int64 */
            cache_read: number;
            /** Format: int64 */
            cache_write: number;
            /** Format: int64 */
            messages: number;
        };
        AgentItem: {
            agent_id: string;
            agent_type: string;
            description: string;
            parent_agent_id: string | null;
            spawn_tool_use_id: string | null;
            spawn_depth: number;
            /** Format: date-time */
            started_at: string | null;
            /** Format: date-time */
            ended_at: string | null;
            finished: boolean | null;
            /** Format: int64 */
            duration_ms: number;
            duration_estimated: boolean;
            tool_calls: {
                [key: string]: number;
            };
            /** Format: int64 */
            tool_errors: number;
            usage: components["schemas"]["AgentUsage"];
        };
        PhaseItem: {
            agent_id: string;
            seq: number;
            skill: string;
            /** Format: date-time */
            started_at: string | null;
            /** Format: date-time */
            ended_at: string | null;
        };
        FailureItem: {
            category: string;
            detail: string;
            /** Format: date-time */
            occurred_at: string;
            session_id: string;
            agent_id: string;
            limit_type: string | null;
            /** Format: date-time */
            resets_at: string | null;
            /** Format: int64 */
            time_lost_s: number | null;
            /**
             * Format: int64
             * @description raw_lines id of the failing line (0 if unavailable).
             */
            line_id: number;
        };
        SessionDetail: {
            session: components["schemas"]["SessionItem"];
            agents: components["schemas"]["AgentItem"][];
            skill_phases: components["schemas"]["PhaseItem"][];
            failures: components["schemas"]["FailureItem"][];
        };
        LineItem: {
            /** Format: int64 */
            id: number;
            /** Format: int64 */
            offset: number;
            /** @description The raw transcript line. */
            line: Record<string, never>;
        };
        LinePage: {
            lines: components["schemas"]["LineItem"][];
            next: string | null;
            /** @description Id of the first returned line when an earlier line of the agent exists. */
            prev: string | null;
        };
        FailureAgg: {
            key: string;
            /** Format: int64 */
            count: number;
            /** Format: int64 */
            time_lost_s: number;
        };
        FailurePage: {
            items: components["schemas"]["FailureItem"][];
            next: string | null;
            by_category: components["schemas"]["FailureAgg"][];
            by_limit_type: components["schemas"]["FailureAgg"][];
            by_day: components["schemas"]["FailureAgg"][];
            /** Format: int64 */
            time_lost_s: number;
        };
        SearchHit: {
            session_id: string;
            agent_id: string;
            role: string;
            /** Format: date-time */
            at: string | null;
            title: string;
            snippet: string;
            rank: number;
            /**
             * Format: int64
             * @description raw_lines id of the hit line (0 if unavailable).
             */
            line_id: number;
        };
        SearchResult: {
            hits: components["schemas"]["SearchHit"][];
        };
        SelfToday: {
            /** Format: date-time */
            from: string;
            /** Format: date-time */
            to: string;
            time_zone: string;
            /** Format: int64 */
            sessions: number;
            /** Format: int64 */
            agent_ms: number;
            cost_usd: number;
            /** Format: int64 */
            tokens: number;
            /** Format: int64 */
            failures: number;
            /** Format: int64 */
            time_lost_s: number;
        };
        SelfSession: {
            session_id: string;
            /** Format: int64 */
            agent_ms: number;
            /** Format: int64 */
            total_agent_ms: number;
            cost_usd: number;
            /** Format: int64 */
            tokens: number;
            /** Format: date-time */
            started_at: string | null;
            /** Format: date-time */
            ended_at: string | null;
        };
        Self: {
            today: components["schemas"]["SelfToday"];
            session: components["schemas"]["SelfSession"] | null;
        };
        AdminUserStats: {
            /** Format: uuid */
            id: string;
            email: string;
            name: string;
            is_admin: boolean;
            /** Format: date-time */
            disabled_at: string | null;
            /** Format: date-time */
            last_login_at: string | null;
            /** Format: date-time */
            last_sync_at: string | null;
            devices: number;
            /** Format: int64 */
            stored_bytes: number;
            /** Format: int64 */
            sessions: number;
            cost_usd: number;
            /** Format: int64 */
            tokens: number;
            /** Format: date-time */
            last_activity_at: string | null;
            /** Format: int64 */
            derive_errors: number;
            rebuilding: boolean;
        };
        AdminTotals: {
            /** Format: int64 */
            users: number;
            /** Format: int64 */
            devices: number;
            /** Format: int64 */
            sessions: number;
            /** Format: int64 */
            stored_bytes: number;
            cost_usd: number;
            /** Format: int64 */
            tokens: number;
        };
        AdminStats: {
            users: components["schemas"]["AdminUserStats"][];
            totals: components["schemas"]["AdminTotals"];
            rebuilding_users: number;
        };
    };
    responses: {
        /** @description Error */
        Error: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["Error"];
            };
        };
    };
    parameters: {
        Id: string;
        /** @description Return entries with id below this cursor. */
        Before: number;
        Limit: number;
        /** @description Free-text session id, not a uuid. */
        SessionId: string;
        /** @description Range start (RFC 3339); default 30 days before To. */
        From: string;
        /** @description Range end (RFC 3339); default now. */
        To: string;
        DeviceFilter: string;
        ProjectFilter: string;
        ModelFilter: string;
        SkillFilter: string;
        /** @description Opaque cursor from the previous page's next. */
        Cursor: string;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    getAbout: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["About"];
                };
            };
            401: components["responses"]["Error"];
        };
    };
    liveSocket: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Switching Protocols */
            101: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            401: components["responses"]["Error"];
            403: components["responses"]["Error"];
            429: components["responses"]["Error"];
            503: components["responses"]["Error"];
        };
    };
    getMe: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Me"];
                };
            };
            401: components["responses"]["Error"];
        };
    };
    patchMe: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": {
                    /** @description IANA zone name. */
                    time_zone: string;
                };
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Me"];
                };
            };
            400: components["responses"]["Error"];
            401: components["responses"]["Error"];
            403: components["responses"]["Error"];
            404: components["responses"]["Error"];
        };
    };
    listDevices: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Device"][];
                };
            };
            401: components["responses"]["Error"];
        };
    };
    revokeDevice: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["Id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Revoked */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            403: components["responses"]["Error"];
            404: components["responses"]["Error"];
        };
    };
    renameDevice: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["Id"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": {
                    name: string;
                };
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Device"];
                };
            };
            400: components["responses"]["Error"];
            403: components["responses"]["Error"];
            404: components["responses"]["Error"];
        };
    };
    listAudit: {
        parameters: {
            query?: {
                /** @description Return entries with id below this cursor. */
                before?: components["parameters"]["Before"];
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AuditPage"];
                };
            };
            400: components["responses"]["Error"];
        };
    };
    adminListUsers: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AdminUser"][];
                };
            };
            403: components["responses"]["Error"];
        };
    };
    adminDisableUser: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["Id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Disabled */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            400: components["responses"]["Error"];
            403: components["responses"]["Error"];
            404: components["responses"]["Error"];
        };
    };
    adminEnableUser: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["Id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Enabled */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            400: components["responses"]["Error"];
            403: components["responses"]["Error"];
            404: components["responses"]["Error"];
        };
    };
    adminDeleteUser: {
        parameters: {
            query: {
                /** @description Must equal the user's email exactly. */
                confirm: string;
            };
            header?: never;
            path: {
                id: components["parameters"]["Id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Deleted */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            400: components["responses"]["Error"];
            403: components["responses"]["Error"];
            404: components["responses"]["Error"];
        };
    };
    adminListAudit: {
        parameters: {
            query?: {
                /** @description Return entries with id below this cursor. */
                before?: components["parameters"]["Before"];
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AuditPage"];
                };
            };
            403: components["responses"]["Error"];
        };
    };
    statsLive: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["LivePage"];
                };
            };
            401: components["responses"]["Error"];
        };
    };
    statsFilters: {
        parameters: {
            query?: {
                /** @description Range start (RFC 3339); default 30 days before To. */
                from?: components["parameters"]["From"];
                /** @description Range end (RFC 3339); default now. */
                to?: components["parameters"]["To"];
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FilterOptions"];
                };
            };
            400: components["responses"]["Error"];
            401: components["responses"]["Error"];
        };
    };
    statsOverview: {
        parameters: {
            query?: {
                /** @description Range start (RFC 3339); default 30 days before To. */
                from?: components["parameters"]["From"];
                /** @description Range end (RFC 3339); default now. */
                to?: components["parameters"]["To"];
                device?: components["parameters"]["DeviceFilter"];
                project?: components["parameters"]["ProjectFilter"];
                model?: components["parameters"]["ModelFilter"];
                skill?: components["parameters"]["SkillFilter"];
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Overview"];
                };
            };
            400: components["responses"]["Error"];
            401: components["responses"]["Error"];
        };
    };
    statsSessions: {
        parameters: {
            query?: {
                /** @description Range start (RFC 3339); default 30 days before To. */
                from?: components["parameters"]["From"];
                /** @description Range end (RFC 3339); default now. */
                to?: components["parameters"]["To"];
                device?: components["parameters"]["DeviceFilter"];
                project?: components["parameters"]["ProjectFilter"];
                model?: components["parameters"]["ModelFilter"];
                skill?: components["parameters"]["SkillFilter"];
                /** @description Opaque cursor from the previous page's next. */
                cursor?: components["parameters"]["Cursor"];
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionPage"];
                };
            };
            400: components["responses"]["Error"];
            401: components["responses"]["Error"];
        };
    };
    statsSession: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Free-text session id, not a uuid. */
                id: components["parameters"]["SessionId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionDetail"];
                };
            };
            400: components["responses"]["Error"];
            401: components["responses"]["Error"];
            404: components["responses"]["Error"];
        };
    };
    statsSessionLines: {
        parameters: {
            query?: {
                /** @description Agent id. */
                agent?: string;
                /** @description Return lines with id above this line id. */
                after?: number;
                /** @description Return the newest lines with id below this line id */
                before?: number;
                limit?: number;
            };
            header?: never;
            path: {
                /** @description Free-text session id, not a uuid. */
                id: components["parameters"]["SessionId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["LinePage"];
                };
            };
            400: components["responses"]["Error"];
            401: components["responses"]["Error"];
            404: components["responses"]["Error"];
        };
    };
    statsFailures: {
        parameters: {
            query?: {
                /** @description Range start (RFC 3339); default 30 days before To. */
                from?: components["parameters"]["From"];
                /** @description Range end (RFC 3339); default now. */
                to?: components["parameters"]["To"];
                device?: components["parameters"]["DeviceFilter"];
                project?: components["parameters"]["ProjectFilter"];
                model?: components["parameters"]["ModelFilter"];
                skill?: components["parameters"]["SkillFilter"];
                /** @description Opaque cursor from the previous page's next. */
                cursor?: components["parameters"]["Cursor"];
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FailurePage"];
                };
            };
            400: components["responses"]["Error"];
            401: components["responses"]["Error"];
        };
    };
    statsSearch: {
        parameters: {
            query: {
                /** @description 2 to 200 characters. */
                q: string;
                /** @description Range start (RFC 3339); default 30 days before To. */
                from?: components["parameters"]["From"];
                /** @description Range end (RFC 3339); default now. */
                to?: components["parameters"]["To"];
                device?: components["parameters"]["DeviceFilter"];
                project?: components["parameters"]["ProjectFilter"];
                model?: components["parameters"]["ModelFilter"];
                skill?: components["parameters"]["SkillFilter"];
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SearchResult"];
                };
            };
            400: components["responses"]["Error"];
            401: components["responses"]["Error"];
        };
    };
    adminStats: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AdminStats"];
                };
            };
            401: components["responses"]["Error"];
            403: components["responses"]["Error"];
        };
    };
    statsSelf: {
        parameters: {
            query?: {
                /** @description Session id (max 256 characters). */
                session?: string;
            };
            header: {
                "Braid-Protocol": string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Self"];
                };
            };
            400: components["responses"]["Error"];
            401: components["responses"]["Error"];
            403: components["responses"]["Error"];
        };
    };
}
