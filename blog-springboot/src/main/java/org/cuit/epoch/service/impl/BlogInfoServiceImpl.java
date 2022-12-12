package org.cuit.epoch.service.impl;

import com.alibaba.fastjson.JSON;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import org.cuit.epoch.dto.blog.BlogHomeInfoDTO;
import org.cuit.epoch.entity.Article;
import org.cuit.epoch.mapper.*;
import org.cuit.epoch.service.BlogInfoService;
import org.cuit.epoch.service.PageService;
import org.cuit.epoch.service.RedisService;
import org.cuit.epoch.vo.PageVO;
import org.cuit.epoch.vo.WebsiteConfigVO;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

import javax.annotation.Resource;
import javax.servlet.http.HttpServletRequest;
import java.util.*;

import static org.cuit.epoch.enums.ArticleStatusEnum.PUBLIC;
import static org.cuit.epoch.constant.CommonConst.DEFAULT_CONFIG_ID;
import static org.cuit.epoch.constant.CommonConst.FALSE;
import static org.cuit.epoch.constant.RedisPrefixConst.BLOG_VIEWS_COUNT;
import static org.cuit.epoch.constant.RedisPrefixConst.WEBSITE_CONFIG;

/**
 * @author: ladidol
 * @date: 2022/11/16 19:10
 * @description:
 */
@Service
public class BlogInfoServiceImpl implements BlogInfoService {
    @Autowired
    private UserInfoMapper userInfoMapper;
    @Autowired
    private ArticleMapper articleDao;
    @Autowired
    private CategoryMapper categoryDao;
    @Autowired
    private TagMapper tagDao;
//    @Autowired
//    private MessageMapper messageDao;
//    @Autowired
//    private UniqueViewService uniqueViewService;
    @Autowired
    private RedisService redisService;
    @Autowired
    private WebsiteConfigMapper websiteConfigDao;
    @Resource
    private HttpServletRequest request;
    @Autowired
    private PageService pageService;

    @Override
    public BlogHomeInfoDTO getBlogHomeInfo() {
        // 查询文章数量
        Integer articleCount = articleDao.selectCount(new LambdaQueryWrapper<Article>()
                .eq(Article::getStatus, PUBLIC.getStatus())
                .eq(Article::getIsDelete, FALSE));
        // 查询分类数量
        Integer categoryCount = categoryDao.selectCount(null);
        // 查询标签数量
        Integer tagCount = tagDao.selectCount(null);
        // 查询访问量
        Object count = redisService.get(BLOG_VIEWS_COUNT);
        String viewsCount = Optional.ofNullable(count).orElse(0).toString();
        // 查询网站配置
        WebsiteConfigVO websiteConfig = this.getWebsiteConfig();
        // 查询页面图片
        List<PageVO> pageVOList = pageService.listPages();
        // 封装数据
        return BlogHomeInfoDTO.builder()
                .articleCount(articleCount)
                .categoryCount(categoryCount)
                .tagCount(tagCount)
                .viewsCount(viewsCount)
                .websiteConfig(websiteConfig)
                .pageList(pageVOList)
                .build();
    }

//    @Override
//    public BlogBackInfoDTO getBlogBackInfo() {
//        // 查询访问量
//        Object count = redisService.get(BLOG_VIEWS_COUNT);
//        Integer viewsCount = Integer.parseInt(Optional.ofNullable(count).orElse(0).toString());
//        // 查询留言量
//        Integer messageCount = messageDao.selectCount(null);
//        // 查询用户量
//        Integer userCount = userInfoDao.selectCount(null);
//        // 查询文章量
//        Integer articleCount = articleDao.selectCount(new LambdaQueryWrapper<Article>()
//                .eq(Article::getIsDelete, FALSE));
//        // 查询一周用户量
//        List<UniqueViewDTO> uniqueViewList = uniqueViewService.listUniqueViews();
//        // 查询文章统计
//        List<ArticleStatisticsDTO> articleStatisticsList = articleDao.listArticleStatistics();
//        // 查询分类数据
//        List<CategoryDTO> categoryDTOList = categoryDao.listCategoryDTO();
//        // 查询标签数据
//        List<TagDTO> tagDTOList = BeanCopyUtils.copyList(tagDao.selectList(null), TagDTO.class);
//        // 查询redis访问量前五的文章
//        Map<Object, Double> articleMap = redisService.zReverseRangeWithScore(ARTICLE_VIEWS_COUNT, 0, 4);
//        BlogBackInfoDTO blogBackInfoDTO = BlogBackInfoDTO.builder()
//                .articleStatisticsList(articleStatisticsList)
//                .tagDTOList(tagDTOList)
//                .viewsCount(viewsCount)
//                .messageCount(messageCount)
//                .userCount(userCount)
//                .articleCount(articleCount)
//                .categoryDTOList(categoryDTOList)
//                .uniqueViewDTOList(uniqueViewList)
//                .build();
//        if (CollectionUtils.isNotEmpty(articleMap)) {
//            // 查询文章排行
//            List<ArticleRankDTO> articleRankDTOList = listArticleRank(articleMap);
//            blogBackInfoDTO.setArticleRankDTOList(articleRankDTOList);
//        }
//        return blogBackInfoDTO;
//    }

//    @Override
//    public void updateWebsiteConfig(WebsiteConfigVO websiteConfigVO) {
//        // 修改网站配置
//        WebsiteConfig websiteConfig = WebsiteConfig.builder()
//                .id(1)
//                .config(JSON.toJSONString(websiteConfigVO))
//                .build();
//        websiteConfigDao.updateById(websiteConfig);
//        // 删除缓存
//        redisService.del(WEBSITE_CONFIG);
//    }

    @Override
    public WebsiteConfigVO getWebsiteConfig() {
        WebsiteConfigVO websiteConfigVO;
        // 获取缓存数据
        Object websiteConfig = redisService.get(WEBSITE_CONFIG);
        if (Objects.nonNull(websiteConfig)) {
            websiteConfigVO = JSON.parseObject(websiteConfig.toString(), WebsiteConfigVO.class);
        } else {
            // 从数据库中加载
            String config = websiteConfigDao.selectById(DEFAULT_CONFIG_ID).getConfig();
            websiteConfigVO = JSON.parseObject(config, WebsiteConfigVO.class);
            redisService.set(WEBSITE_CONFIG, config);
        }
        return websiteConfigVO;
    }

//    @Override
//    public String getAbout() {
//        Object value = redisService.get(ABOUT);
//        return Objects.nonNull(value) ? value.toString() : "";
//    }
//
//    @Override
//    public void updateAbout(BlogInfoVO blogInfoVO) {
//        redisService.set(ABOUT, blogInfoVO.getAboutContent());
//    }
//
//    @Override
//    public void report() {
//        // 获取ip
//        String ipAddress = IpUtils.getIpAddress(request);
//        // 获取访问设备
//        UserAgent userAgent = IpUtils.getUserAgent(request);
//        Browser browser = userAgent.getBrowser();
//        OperatingSystem operatingSystem = userAgent.getOperatingSystem();
//        // 生成唯一用户标识
//        String uuid = ipAddress + browser.getName() + operatingSystem.getName();
//        String md5 = DigestUtils.md5DigestAsHex(uuid.getBytes());
//        // 判断是否访问
//        if (!redisService.sIsMember(UNIQUE_VISITOR, md5)) {
//            // 统计游客地域分布
//            String ipSource = IpUtils.getIpSource(ipAddress);
//            if (StringUtils.isNotBlank(ipSource)) {
//                ipSource = ipSource.substring(0, 2)
//                        .replaceAll(PROVINCE, "")
//                        .replaceAll(CITY, "");
//                redisService.hIncr(VISITOR_AREA, ipSource, 1L);
//            } else {
//                redisService.hIncr(VISITOR_AREA, UNKNOWN, 1L);
//            }
//            // 访问量+1
//            redisService.incr(BLOG_VIEWS_COUNT, 1);
//            // 保存唯一标识
//            redisService.sAdd(UNIQUE_VISITOR, md5);
//        }
//    }
//
//    /**
//     * 查询文章排行
//     *
//     * @param articleMap 文章信息
//     * @return {@link List<ArticleRankDTO>} 文章排行
//     */
//    private List<ArticleRankDTO> listArticleRank(Map<Object, Double> articleMap) {
//        // 提取文章id
//        List<Integer> articleIdList = new ArrayList<>(articleMap.size());
//        articleMap.forEach((key, value) -> articleIdList.add((Integer) key));
//        // 查询文章信息
//        return articleDao.selectList(new LambdaQueryWrapper<Article>()
//                        .select(Article::getId, Article::getArticleTitle)
//                        .in(Article::getId, articleIdList))
//                .stream().map(article -> ArticleRankDTO.builder()
//                        .articleTitle(article.getArticleTitle())
//                        .viewsCount(articleMap.get(article.getId()).intValue())
//                        .build())
//                .sorted(Comparator.comparingInt(ArticleRankDTO::getViewsCount).reversed())
//                .collect(Collectors.toList());
//    }

}