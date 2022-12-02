package org.cuit.epoch.enums;

/**
 * @author: ladidol
 * @date: 2022/11/16 17:19
 * @description:
 */
public class RedisPrefixConst {

    /**
     * 验证码过期时间
     */
    public static final long CODE_EXPIRE_TIME = 15 * 60;

    /**
     * 验证码
     */
    public static final String USER_CODE_KEY = "new_code:";

    /**
     * 博客浏览量
     */
    public static final String BLOG_VIEWS_COUNT = "new_blog_views_count";

    /**
     * 文章浏览量
     */
    public static final String ARTICLE_VIEWS_COUNT = "new_article_views_count";

    /**
     * 文章点赞量
     */
    public static final String ARTICLE_LIKE_COUNT = "new_article_like_count";

    /**
     * 用户点赞文章
     */
    public static final String ARTICLE_USER_LIKE = "new_article_user_like:";

    /**
     * 说说点赞量
     */
    public static final String TALK_LIKE_COUNT = "new_talk_like_count";

    /**
     * 用户点赞说说
     */
    public static final String TALK_USER_LIKE = "new_talk_user_like:";

    /**
     * 评论点赞量
     */
    public static final String COMMENT_LIKE_COUNT = "new_comment_like_count";

    /**
     * 用户点赞评论
     */
    public static final String COMMENT_USER_LIKE = "new_comment_user_like:";

    /**
     * 网站配置
     */
    public static final String WEBSITE_CONFIG = "new_website_config";

    /**
     * 用户地区
     */
    public static final String USER_AREA = "new_user_area";

    /**
     * 访客地区
     */
    public static final String VISITOR_AREA = "new_visitor_area";

    /**
     * 页面封面
     */
    public static final String PAGE_COVER = "new_page_cover";

    /**
     * 关于我信息
     */
    public static final String ABOUT = "new_about";

    /**
     * 访客
     */
    public static final String UNIQUE_VISITOR = "new_unique_visitor";

    /**
     * 用户角色信息
     *
     */
    public static final String USER_ROLE = "new_user_role";

    /**
     * 用户详细信息
     *
     */
    public static final String USER_INFO = "new_user_info";

    /**
     * 用户详细信息
     *
     */
    public static final String USER_ONLINE = "new_user_online";



}