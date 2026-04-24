-- TradeFlow schema init
-- Runs automatically on first `docker compose up` (empty volume only)

SET statement_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET check_function_bodies = false;
SET client_min_messages = warning;
SET row_security = off;

-- ── clients ──────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS public.clients (
    id       integer NOT NULL,
    username text    NOT NULL,
    password text    NOT NULL
);

CREATE SEQUENCE IF NOT EXISTS public.clients_id_seq
    AS integer START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

ALTER SEQUENCE public.clients_id_seq OWNED BY public.clients.id;
ALTER TABLE ONLY public.clients ALTER COLUMN id SET DEFAULT nextval('public.clients_id_seq'::regclass);
ALTER TABLE ONLY public.clients ADD CONSTRAINT clients_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.clients ADD CONSTRAINT clients_username_key UNIQUE (username);

-- ── client_subscriptions ─────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS public.client_subscriptions (
    id            integer NOT NULL,
    client_id     integer NOT NULL,
    instrument_id integer NOT NULL,
    subscribed_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE IF NOT EXISTS public.client_subscriptions_id_seq
    AS integer START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

ALTER SEQUENCE public.client_subscriptions_id_seq OWNED BY public.client_subscriptions.id;
ALTER TABLE ONLY public.client_subscriptions ALTER COLUMN id SET DEFAULT nextval('public.client_subscriptions_id_seq'::regclass);
ALTER TABLE ONLY public.client_subscriptions ADD CONSTRAINT client_subscriptions_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.client_subscriptions ADD CONSTRAINT client_subscriptions_client_id_instrument_id_key UNIQUE (client_id, instrument_id);
ALTER TABLE ONLY public.client_subscriptions ADD CONSTRAINT client_subscriptions_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id) ON DELETE CASCADE;

-- ── analytics tables ─────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS public.sma (
    "time"      timestamp with time zone NOT NULL,
    instrument  integer                  NOT NULL,
    resolution  text                     NOT NULL,
    value       double precision         NOT NULL,
    client_id   integer
);
ALTER TABLE ONLY public.sma ADD CONSTRAINT sma_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);

CREATE TABLE IF NOT EXISTS public.ema (
    "time"      timestamp with time zone NOT NULL,
    instrument  integer                  NOT NULL,
    resolution  text                     NOT NULL,
    value       double precision         NOT NULL,
    client_id   integer
);
ALTER TABLE ONLY public.ema ADD CONSTRAINT ema_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);

CREATE TABLE IF NOT EXISTS public.ohlc (
    "time"      timestamp with time zone NOT NULL,
    instrument  integer                  NOT NULL,
    resolution  text                     NOT NULL,
    open        double precision         NOT NULL,
    high        double precision         NOT NULL,
    low         double precision         NOT NULL,
    close       double precision         NOT NULL,
    client_id   integer
);
ALTER TABLE ONLY public.ohlc ADD CONSTRAINT ohlc_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);

CREATE TABLE IF NOT EXISTS public.bb (
    "time"      timestamp with time zone NOT NULL,
    instrument  integer                  NOT NULL,
    resolution  text                     NOT NULL,
    upper       double precision         NOT NULL,
    middle      double precision         NOT NULL,
    lower       double precision         NOT NULL,
    client_id   integer
);
ALTER TABLE ONLY public.bb ADD CONSTRAINT bb_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);

CREATE TABLE IF NOT EXISTS public.rsi (
    "time"      timestamp with time zone NOT NULL,
    instrument  integer                  NOT NULL,
    resolution  text                     NOT NULL,
    value       double precision         NOT NULL,
    client_id   integer
);
ALTER TABLE ONLY public.rsi ADD CONSTRAINT rsi_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);

CREATE TABLE IF NOT EXISTS public.macd (
    "time"      timestamp with time zone NOT NULL,
    instrument  integer                  NOT NULL,
    resolution  text                     NOT NULL,
    macd_line   double precision         NOT NULL,
    signal_line double precision         NOT NULL,
    histogram   double precision         NOT NULL,
    client_id   integer
);
ALTER TABLE ONLY public.macd ADD CONSTRAINT macd_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);