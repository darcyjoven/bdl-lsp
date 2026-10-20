-- 样例：最小 BDL 模块
SCHEMA ds
GLOBALS "globals.4gl"

DEFINE g_count LIKE type_file.num5
DEFINE g_name  LIKE type_file.chr20

MAIN
    LET g_count = 0
    CALL show_name()
END MAIN

FUNCTION show_name()
    DEFINE l_msg STRING

    LET l_msg = "name: ", g_name
    DISPLAY l_msg
END FUNCTION
