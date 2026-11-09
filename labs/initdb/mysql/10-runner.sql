-- Профиль labs: пользователь, от которого runner создаёт временные базы
-- запусков. Права — только на базы run_* и на то, что нужно для уборки.
CREATE USER 'runner'@'%' IDENTIFIED BY 'labs-runner';
GRANT CREATE USER, PROCESS, CONNECTION_ADMIN ON *.* TO 'runner'@'%';
-- Шаблон без экранирования «_»: с `run\_%` MySQL 8.4 создаёт базу, но не даёт
-- выдать на неё права (GRANT ... ON run_x.* — ошибка 1044).
GRANT ALL PRIVILEGES ON `run_%`.* TO 'runner'@'%' WITH GRANT OPTION;
GRANT SELECT ON mysql.user TO 'runner'@'%';
-- Урок о блокировках InnoDB читает только эти две таблицы.
GRANT SELECT ON performance_schema.data_locks TO 'runner'@'%' WITH GRANT OPTION;
GRANT SELECT ON performance_schema.data_lock_waits TO 'runner'@'%' WITH GRANT OPTION;

-- root из сети песочницы (подсеть задана в docker-compose.yml) попадает
-- на эту запись — более точный хост, чем '%', — а она заблокирована.
CREATE USER 'root'@'172.29.99.%' IDENTIFIED BY RANDOM PASSWORD ACCOUNT LOCK;
-- CREATE USER у runner позволяет DROP/ALTER USER над любой учётной записью
-- без SYSTEM_USER: программа ученика удалила бы блокировку и вошла как root.
-- Записи с SYSTEM_USER может менять только пользователь с SYSTEM_USER.
GRANT SYSTEM_USER ON *.* TO 'root'@'172.29.99.%';
