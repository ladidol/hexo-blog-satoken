package org.cuit.epoch.dto;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;
import org.cuit.epoch.vo.PageVO;
import org.cuit.epoch.vo.WebsiteConfigVO;

import java.util.List;

/**
 * @author: ladidol
 * @date: 2022/11/16 18:55
 * @description:
 */
@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class BlogHomeInfoDTO {

    /**
     * 文章数量
     */
    private Integer articleCount;

    /**
     * 分类数量
     */
    private Integer categoryCount;

    /**
     * 标签数量
     */
    private Integer tagCount;

    /**
     * 访问量
     */
    private String viewsCount;

    /**
     * 网站配置
     */
    private WebsiteConfigVO websiteConfig;

    /**
     * 页面列表
     */
    private List<PageVO> pageList;

}