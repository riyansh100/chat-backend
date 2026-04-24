--
-- PostgreSQL database dump
--

\restrict EbtSCHallGbDyQUG9ot6nJoguBSV20UE95Jg7HqRogSdQmAfVk5MR5erAwGQkCO

-- Dumped from database version 18.3 (Homebrew)
-- Dumped by pg_dump version 18.3 (Homebrew)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: bb; Type: TABLE; Schema: public; Owner: riyanshsachdev
--

CREATE TABLE public.bb (
    "time" timestamp with time zone NOT NULL,
    instrument integer NOT NULL,
    resolution text NOT NULL,
    upper double precision NOT NULL,
    middle double precision NOT NULL,
    lower double precision NOT NULL,
    client_id integer
);


ALTER TABLE public.bb OWNER TO riyanshsachdev;

--
-- Name: client_subscriptions; Type: TABLE; Schema: public; Owner: riyanshsachdev
--

CREATE TABLE public.client_subscriptions (
    id integer NOT NULL,
    client_id integer NOT NULL,
    instrument_id integer NOT NULL,
    subscribed_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.client_subscriptions OWNER TO riyanshsachdev;

--
-- Name: client_subscriptions_id_seq; Type: SEQUENCE; Schema: public; Owner: riyanshsachdev
--

CREATE SEQUENCE public.client_subscriptions_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.client_subscriptions_id_seq OWNER TO riyanshsachdev;

--
-- Name: client_subscriptions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: riyanshsachdev
--

ALTER SEQUENCE public.client_subscriptions_id_seq OWNED BY public.client_subscriptions.id;


--
-- Name: clients; Type: TABLE; Schema: public; Owner: riyanshsachdev
--

CREATE TABLE public.clients (
    id integer NOT NULL,
    username text NOT NULL,
    password text NOT NULL
);


ALTER TABLE public.clients OWNER TO riyanshsachdev;

--
-- Name: clients_id_seq; Type: SEQUENCE; Schema: public; Owner: riyanshsachdev
--

CREATE SEQUENCE public.clients_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.clients_id_seq OWNER TO riyanshsachdev;

--
-- Name: clients_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: riyanshsachdev
--

ALTER SEQUENCE public.clients_id_seq OWNED BY public.clients.id;


--
-- Name: ema; Type: TABLE; Schema: public; Owner: riyanshsachdev
--

CREATE TABLE public.ema (
    "time" timestamp with time zone NOT NULL,
    instrument integer NOT NULL,
    resolution text NOT NULL,
    value double precision NOT NULL,
    client_id integer
);


ALTER TABLE public.ema OWNER TO riyanshsachdev;

--
-- Name: macd; Type: TABLE; Schema: public; Owner: riyanshsachdev
--

CREATE TABLE public.macd (
    "time" timestamp with time zone NOT NULL,
    instrument integer NOT NULL,
    resolution text NOT NULL,
    macd_line double precision NOT NULL,
    signal_line double precision NOT NULL,
    histogram double precision NOT NULL,
    client_id integer
);


ALTER TABLE public.macd OWNER TO riyanshsachdev;

--
-- Name: ohlc; Type: TABLE; Schema: public; Owner: riyanshsachdev
--

CREATE TABLE public.ohlc (
    "time" timestamp with time zone NOT NULL,
    instrument integer NOT NULL,
    resolution text NOT NULL,
    open double precision NOT NULL,
    high double precision NOT NULL,
    low double precision NOT NULL,
    close double precision NOT NULL,
    client_id integer
);


ALTER TABLE public.ohlc OWNER TO riyanshsachdev;

--
-- Name: rsi; Type: TABLE; Schema: public; Owner: riyanshsachdev
--

CREATE TABLE public.rsi (
    "time" timestamp with time zone NOT NULL,
    instrument integer NOT NULL,
    resolution text NOT NULL,
    value double precision NOT NULL,
    client_id integer
);


ALTER TABLE public.rsi OWNER TO riyanshsachdev;

--
-- Name: sma; Type: TABLE; Schema: public; Owner: riyanshsachdev
--

CREATE TABLE public.sma (
    "time" timestamp with time zone NOT NULL,
    instrument integer NOT NULL,
    resolution text NOT NULL,
    value double precision NOT NULL,
    client_id integer
);


ALTER TABLE public.sma OWNER TO riyanshsachdev;

--
-- Name: client_subscriptions id; Type: DEFAULT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.client_subscriptions ALTER COLUMN id SET DEFAULT nextval('public.client_subscriptions_id_seq'::regclass);


--
-- Name: clients id; Type: DEFAULT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.clients ALTER COLUMN id SET DEFAULT nextval('public.clients_id_seq'::regclass);


--
-- Name: client_subscriptions client_subscriptions_client_id_instrument_id_key; Type: CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.client_subscriptions
    ADD CONSTRAINT client_subscriptions_client_id_instrument_id_key UNIQUE (client_id, instrument_id);


--
-- Name: client_subscriptions client_subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.client_subscriptions
    ADD CONSTRAINT client_subscriptions_pkey PRIMARY KEY (id);


--
-- Name: clients clients_pkey; Type: CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.clients
    ADD CONSTRAINT clients_pkey PRIMARY KEY (id);


--
-- Name: clients clients_username_key; Type: CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.clients
    ADD CONSTRAINT clients_username_key UNIQUE (username);


--
-- Name: bb bb_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.bb
    ADD CONSTRAINT bb_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);


--
-- Name: client_subscriptions client_subscriptions_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.client_subscriptions
    ADD CONSTRAINT client_subscriptions_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id) ON DELETE CASCADE;


--
-- Name: ema ema_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.ema
    ADD CONSTRAINT ema_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);


--
-- Name: macd macd_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.macd
    ADD CONSTRAINT macd_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);


--
-- Name: ohlc ohlc_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.ohlc
    ADD CONSTRAINT ohlc_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);


--
-- Name: rsi rsi_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.rsi
    ADD CONSTRAINT rsi_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);


--
-- Name: sma sma_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: riyanshsachdev
--

ALTER TABLE ONLY public.sma
    ADD CONSTRAINT sma_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id);


--
-- PostgreSQL database dump complete
--

\unrestrict EbtSCHallGbDyQUG9ot6nJoguBSV20UE95Jg7HqRogSdQmAfVk5MR5erAwGQkCO

