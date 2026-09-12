CREATE TABLE tb_page (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  page_name TEXT NOT NULL,
  page_label TEXT NULL DEFAULT NULL,
  page_cover TEXT NOT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  UNIQUE (page_label)
);

CREATE TABLE tb_website_config (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  config TEXT NULL DEFAULT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL
);

CREATE TABLE tb_about (
  id INTEGER PRIMARY KEY CHECK(id=1), content TEXT NOT NULL DEFAULT '',
  create_time TEXT NOT NULL, update_time TEXT
);
