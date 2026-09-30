-- AITS division assessment seed update
-- Date: 2026-10-01
-- Purpose:
--   1. Ensure CodeHex, Illustrados, and Neo-Tech have at least 50 seeded questions each.
--   2. Remove the explicitly requested DEVTEST member_profiles row.
-- Safe to run repeatedly: question inserts are idempotent by seed_key.

BEGIN;

INSERT INTO assessment_questions(
    seed_key,category,division_id,prompt,question_text,correct_answer,dummy_answers,is_active,sort_order,assessment_kind,
    question_content,correct_answer_content,dummy_answer_contents,assessment_metadata,year_level
)
SELECT
    v.seed_key,'division',d.id,v.question,v.question,v.correct_answer,
    jsonb_build_array(v.dummy_1,v.dummy_2,v.dummy_3),TRUE,v.sort_order,'qa',
    jsonb_build_object('type','text','text',v.question),
    jsonb_build_object('type','text','text',v.correct_answer),
    jsonb_build_array(
        jsonb_build_object('type','text','text',v.dummy_1),
        jsonb_build_object('type','text','text',v.dummy_2),
        jsonb_build_object('type','text','text',v.dummy_3)
    ),'{}'::jsonb,v.year_level
FROM (VALUES
    ('application-q-111','codehex',1,111,'Which data type normally represents true or false values?','Boolean','String','Array','Float only'),
    ('application-q-112','codehex',1,112,'Which symbol starts a single-line comment in JavaScript?','//','##','<!--','**'),
    ('application-q-113','codehex',1,113,'What is the main purpose of a loop in programming?','Repeat a block of instructions','Encrypt a database','Create a network cable','Resize an image'),
    ('application-q-114','codehex',2,114,'Which SQL clause is used to filter rows returned by a SELECT query?','WHERE','ORDER','TABLE','COLUMN'),
    ('application-q-115','codehex',2,115,'What does JSON primarily provide?','A text format for structured data','A video codec','A CPU instruction set','A network cable standard'),
    ('application-q-116','codehex',2,116,'Which Git command downloads a repository for the first time?','git clone','git erase','git paint','git compile'),
    ('application-q-117','codehex',3,117,'What is the average lookup complexity of a well-designed hash table?','O(1)','O(n!)','O(2^n)','O(n^3)'),
    ('application-q-118','codehex',3,118,'Which HTTP status code commonly means Not Found?','404','200','301','500'),
    ('application-q-119','codehex',3,119,'Why are prepared statements useful in database applications?','They help prevent SQL injection','They increase monitor brightness','They replace all indexes','They disable authentication'),
    ('application-q-120','codehex',4,120,'What does ACID describe in relational databases?','Transaction reliability properties','A CSS layout method','A network connector','A video compression format'),
    ('application-q-121','codehex',4,121,'Which principle recommends depending on abstractions rather than concrete implementations?','Dependency Inversion Principle','First Come First Served','Least Recently Used','Round Robin'),
    ('application-q-122','codehex',4,122,'What is the purpose of a database index?','Speed up selected data lookups','Encrypt every row automatically','Replace backups','Remove primary keys'),
    ('application-q-123','illustrados',1,123,'Which design principle helps create visual order by lining elements up consistently?','Alignment','Compression','Routing','Compilation'),
    ('application-q-124','illustrados',1,124,'What does contrast help users do in a visual interface?','Distinguish important elements','Increase file size','Change IP addresses','Compile code'),
    ('application-q-125','illustrados',1,125,'Which file format commonly supports transparent backgrounds for raster images?','PNG','TXT','CSV','WAV'),
    ('application-q-126','illustrados',2,126,'In UI design, what is whitespace primarily used for?','Improve clarity and grouping','Store hidden passwords','Increase CPU speed','Replace navigation'),
    ('application-q-127','illustrados',2,127,'What is the purpose of a mood board in visual design?','Collect visual references that communicate a desired style or direction','Store application passwords','Measure network latency','Compile source code'),
    ('application-q-128','illustrados',2,128,'Which color model is primarily used for screens?','RGB','CMYK only','ASCII','MIDI'),
    ('application-q-129','illustrados',3,129,'What is the main goal of responsive design?','Adapt layouts to different screen sizes','Force all screens to one resolution','Disable mobile input','Remove typography'),
    ('application-q-130','illustrados',3,130,'Which accessibility practice improves readability for text over backgrounds?','Maintain sufficient color contrast','Use very small text','Hide focus indicators','Rely on color alone'),
    ('application-q-131','illustrados',3,131,'What is a component library used for in interface design?','Reuse consistent interface elements across screens and products','Replace user research','Store database backups','Configure network routes'),
    ('application-q-132','illustrados',4,132,'What is progressive disclosure in interface design?','Show advanced details only when they are needed','Display every control at once','Hide all navigation permanently','Disable user feedback'),
    ('application-q-133','illustrados',4,133,'Why is consistent component behavior important in UX?','It makes interactions predictable','It increases random variation','It removes accessibility needs','It prevents responsive layouts'),
    ('application-q-134','illustrados',4,134,'What does usability testing primarily evaluate?','How real users complete tasks with a design','Only server CPU temperature','Only image resolution','Only source-code line count'),
    ('application-q-135','neo-tech',1,135,'Which component stores data temporarily while programs are running?','RAM','Power supply','Case fan','HDMI cable'),
    ('application-q-136','neo-tech',1,136,'What is the main purpose of an operating system?','Manage hardware and provide services to applications','Replace the CPU','Act only as a web browser','Create Ethernet cables'),
    ('application-q-137','neo-tech',1,137,'Which unit is commonly used to measure network data rate?','bits per second','pixels per inch','degrees Celsius','watts per hour'),
    ('application-q-138','neo-tech',2,138,'Which command is commonly used to test basic reachability to another host?','ping','paint','mkdir only','format'),
    ('application-q-139','neo-tech',2,139,'What does a subnet mask help determine?','Which part of an IP address identifies the network','Screen resolution','File extension','CPU clock multiplier'),
    ('application-q-140','neo-tech',2,140,'Which storage device has no moving mechanical parts?','SSD','Traditional floppy disk','Optical mouse','Ethernet switch'),
    ('application-q-141','neo-tech',3,141,'What is the purpose of NAT on many routers?','Translate between private and public IP addressing','Compress images','Compile programs','Format documents'),
    ('application-q-142','neo-tech',3,142,'Which protocol securely provides remote command-line access?','SSH','HTTP','FTP without TLS','ARP'),
    ('application-q-143','neo-tech',3,143,'What does RAID 1 primarily provide?','Data mirroring','Wireless encryption','CPU overclocking','Image compression'),
    ('application-q-144','neo-tech',4,144,'What is VLAN segmentation used for?','Logically separate network traffic on shared infrastructure','Increase monitor resolution','Replace DNS','Compile firmware'),
    ('application-q-145','neo-tech',4,145,'What does the principle of least privilege mean?','Give only the access required for a task','Give every user administrator access','Disable authentication','Share one password with everyone'),
    ('application-q-146','neo-tech',4,146,'Why is network monitoring useful?','It helps detect performance and security issues','It guarantees zero failures','It replaces backups','It removes the need for updates')
) AS v(seed_key,division_code,year_level,sort_order,question,correct_answer,dummy_1,dummy_2,dummy_3)
JOIN divisions d ON d.code=v.division_code
-- seed_key is protected by a partial UNIQUE index (WHERE seed_key IS NOT NULL),
-- so PostgreSQL conflict inference must include the same predicate.
ON CONFLICT (seed_key) WHERE seed_key IS NOT NULL DO NOTHING;

INSERT INTO assessment_questions(
    seed_key,category,division_id,prompt,question_text,correct_answer,dummy_answers,is_active,sort_order,assessment_kind,
    question_content,correct_answer_content,dummy_answer_contents,assessment_metadata,year_level
)
SELECT
    v.seed_key,'division',d.id,v.question,v.question,v.correct_answer,
    jsonb_build_array(v.dummy_1,v.dummy_2,v.dummy_3),TRUE,v.sort_order,'qa',
    jsonb_build_object('type','text','text',v.question),
    jsonb_build_object('type','text','text',v.correct_answer),
    jsonb_build_array(
        jsonb_build_object('type','text','text',v.dummy_1),
        jsonb_build_object('type','text','text',v.dummy_2),
        jsonb_build_object('type','text','text',v.dummy_3)
    ),'{}'::jsonb,v.year_level
FROM (VALUES
    ('application-q-147','codehex',1,147,'Which JavaScript operator compares both value and type without coercion?','===','==','=','!='),
    ('application-q-148','codehex',1,148,'Why should a form label be associated with its input control?','It improves usability and accessibility','It encrypts the input automatically','It creates a database index','It disables keyboard navigation'),
    ('application-q-149','codehex',2,149,'What is the main purpose of a primary key in a relational table?','Uniquely identify each row','Store only encrypted values','Sort every query automatically','Replace all foreign keys'),
    ('application-q-150','codehex',2,150,'Which HTTP method is commonly used to create a new resource?','POST','GET','HEAD','TRACE'),
    ('application-q-151','codehex',3,151,'What prevents a recursive function from calling itself forever?','A base case','A CSS selector','A database trigger only','A network gateway'),
    ('application-q-152','codehex',3,152,'What is the typical time complexity of binary search on a sorted array?','O(log n)','O(n^2)','O(2^n)','O(n!)'),
    ('application-q-153','codehex',4,153,'What is a primary goal of continuous integration (CI)?','Automatically build and test changes frequently','Replace version control','Disable automated tests','Store passwords in source code'),
    ('application-q-154','codehex',4,154,'What does a transaction rollback do?','Reverses uncommitted transaction changes','Deletes the database schema','Creates a new operating system','Turns a query into an index'),
    ('application-q-155','illustrados',1,155,'What does legibility describe in typography?','How easily individual characters can be recognized','How fast a database query runs','How much RAM an image uses','How many network hops exist'),
    ('application-q-156','illustrados',1,156,'Why are vector graphics useful for logos and icons?','They can scale without losing sharpness','They always contain photographs','They require no file format','They can only be printed in black'),
    ('application-q-157','illustrados',2,157,'In interface design, what is an affordance?','A clue about how an element can be used','A database backup schedule','A type of network cable','A programming compiler flag'),
    ('application-q-158','illustrados',2,158,'What is a user persona used for?','Represent a research-based target user type','Replace all usability testing','Define a database password','Measure CPU temperature'),
    ('application-q-159','illustrados',3,159,'Why should meaningful images have useful alternative text?','So their purpose can be understood when the image is unavailable or not visible','To increase image resolution','To make every image animated','To hide the page title'),
    ('application-q-160','illustrados',3,160,'What is a responsive breakpoint?','A layout threshold where presentation adapts to available space','A permanent server failure','A type of image compression','A database primary key'),
    ('application-q-161','illustrados',4,161,'What are design tokens commonly used to store?','Reusable visual values such as spacing, color, and typography','Raw user passwords','Network packet captures','Compiled executable code'),
    ('application-q-162','illustrados',4,162,'What does an A/B test compare?','Two variants to measure which better supports a defined goal','Two database engines by file size only','Two passwords for one account','Two monitors for brightness only'),
    ('application-q-163','neo-tech',1,163,'Which computer component executes most program instructions?','CPU','Keyboard','Monitor','Ethernet cable'),
    ('application-q-164','neo-tech',1,164,'What is DNS mainly used for?','Translate domain names to network addresses','Increase screen brightness','Compress video files','Format storage drives'),
    ('application-q-165','neo-tech',2,165,'What does DHCP commonly provide to devices joining a network?','Automatic IP configuration','Source-code compilation','Image editing tools','Database table names'),
    ('application-q-166','neo-tech',2,166,'What does a MAC address identify at the data-link layer?','A network interface','A website password','A database column','A document page'),
    ('application-q-167','neo-tech',3,167,'What is a firewall primarily used to do?','Control network traffic according to security rules','Increase monitor resolution','Replace DNS records','Defragment RAM'),
    ('application-q-168','neo-tech',3,168,'What does TLS primarily protect for network applications?','Data in transit','CPU clock speed','Physical cable length','Screen color calibration'),
    ('application-q-169','neo-tech',4,169,'What is a central idea of zero-trust security?','Continuously verify access instead of trusting a network location by default','Give every internal user full administrator access','Disable authentication inside an office','Use one password for all systems'),
    ('application-q-170','neo-tech',4,170,'What does the 3-2-1 backup guideline recommend?','Keep three copies on two media types with one copy off-site','Use three passwords on two accounts and one monitor','Restart three times before every backup','Store every copy on the same disk')
) AS v(seed_key,division_code,year_level,sort_order,question,correct_answer,dummy_1,dummy_2,dummy_3)
JOIN divisions d ON d.code=v.division_code
ON CONFLICT (seed_key) WHERE seed_key IS NOT NULL DO NOTHING;

DELETE FROM member_profiles
WHERE account_id=77 AND member_number='DEVTEST-000077';

COMMIT;

-- Verification: each current division should report at least 50 active seeded questions.
SELECT d.code,d.name,COUNT(q.id) AS active_questions
FROM divisions d
LEFT JOIN assessment_questions q ON q.division_id=d.id AND q.is_active=TRUE
WHERE d.code IN ('codehex','illustrados','neo-tech')
GROUP BY d.id,d.code,d.name
ORDER BY d.sort_order,d.id;

-- Verification: this should return zero rows.
SELECT * FROM member_profiles
WHERE account_id=77 AND member_number='DEVTEST-000077';
