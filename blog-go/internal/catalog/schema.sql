CREATE TABLE tb_category (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  category_name TEXT NOT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  UNIQUE (category_name COLLATE NOCASE)
);

CREATE TABLE tb_friend_link (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  link_name TEXT NOT NULL,
  link_avatar TEXT NOT NULL,
  link_address TEXT NOT NULL,
  link_intro TEXT NOT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL
);

CREATE TABLE tb_tag (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tag_name TEXT NOT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  UNIQUE (tag_name COLLATE NOCASE)
);
