CREATE TABLE tb_menu (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  component TEXT NOT NULL,
  icon TEXT NOT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  order_num INTEGER NOT NULL,
  parent_id INTEGER NULL DEFAULT NULL,
  is_hidden INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_menu_parent ON tb_menu(parent_id,order_num,id);

CREATE TABLE tb_resource (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  resource_name TEXT NOT NULL,
  url TEXT NULL DEFAULT NULL,
  request_method TEXT NULL DEFAULT NULL,
  parent_id INTEGER NULL DEFAULT NULL,
  is_anonymous INTEGER NOT NULL DEFAULT 0,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL
);
CREATE INDEX idx_resource_route ON tb_resource(request_method,url);

CREATE TABLE tb_role (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  role_name TEXT NOT NULL,
  role_label TEXT NOT NULL,
  is_disable INTEGER NOT NULL DEFAULT 0,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  UNIQUE (role_label)
);

CREATE TABLE tb_role_menu (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id INTEGER NULL DEFAULT NULL,
  menu_id INTEGER NULL DEFAULT NULL,
  FOREIGN KEY (role_id) REFERENCES tb_role(id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (menu_id) REFERENCES tb_menu(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (role_id,menu_id)
);
CREATE INDEX idx_role_menu_menu ON tb_role_menu(menu_id,role_id);

CREATE TABLE tb_role_resource (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id INTEGER NULL DEFAULT NULL,
  resource_id INTEGER NULL DEFAULT NULL,
  FOREIGN KEY (role_id) REFERENCES tb_role(id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (resource_id) REFERENCES tb_resource(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (role_id,resource_id)
);
CREATE INDEX idx_role_resource_resource ON tb_role_resource(resource_id,role_id);

CREATE TABLE tb_user_auth (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_info_id INTEGER NOT NULL,
  username TEXT NOT NULL,
  password TEXT NOT NULL,
  login_type INTEGER NOT NULL,
  ip_address TEXT NULL DEFAULT NULL,
  ip_source TEXT NULL DEFAULT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  last_login_time TEXT NULL DEFAULT NULL,
  FOREIGN KEY (user_info_id) REFERENCES tb_user_info(id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (username COLLATE NOCASE)
);
CREATE INDEX idx_user_auth_info ON tb_user_auth(user_info_id);

CREATE TABLE tb_user_info (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NULL DEFAULT NULL,
  nickname TEXT NOT NULL,
  avatar TEXT NOT NULL DEFAULT '',
  intro TEXT NULL DEFAULT NULL,
  web_site TEXT NULL DEFAULT NULL,
  is_disable INTEGER NOT NULL DEFAULT 0,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL
);

CREATE TABLE tb_user_role (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NULL DEFAULT NULL,
  role_id INTEGER NULL DEFAULT NULL,
  FOREIGN KEY (user_id) REFERENCES tb_user_info(id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (role_id) REFERENCES tb_role(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (user_id,role_id)
);
