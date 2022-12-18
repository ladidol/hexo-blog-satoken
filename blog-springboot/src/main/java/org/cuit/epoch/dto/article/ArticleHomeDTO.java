package org.cuit.epoch.dto.article;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;
import org.cuit.epoch.dto.tag.TagDTO;

import java.time.LocalDateTime;
import java.util.List;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/17 22:09
 * @description: {}
 */
@Data
@Builder
@AllArgsConstructor
@NoArgsConstructor
public class ArticleHomeDTO {

    /**
     * id
     */
    private Integer id;

    /**
     * 文章缩略图
     */
    private String articleCover;

    /**
     * 标题
     */
    private String articleTitle;

    /**
     * 内容
     */
    private String articleContent;

    /**
     * 发表时间
     */
    private LocalDateTime createTime;

    /**
     * 是否置顶
     */
    private Integer isTop;

    /**
     * 文章类型
     */
    private Integer type;

    /**
     * 文章分类id
     */
    private Integer categoryId;

    /**
     * 文章分类名
     */
    private String categoryName;

    /**
     * 文章标签
     */
    private List<TagDTO> tagDTOList;

}

